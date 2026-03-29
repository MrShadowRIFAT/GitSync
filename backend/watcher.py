import os
import time
import threading
from watchdog.observers import Observer
from watchdog.events import FileSystemEventHandler
from backend.database import (
    get_workspaces, get_repo_mapping_by_path, get_repo_mapping_by_name, add_repo_mapping, 
    remove_repo_mapping, add_pending_deletion, log_action
)
from backend.config import load_settings
from backend.github_service import GitHubService
from backend.ai_service import AIService
import git

def send_desktop_notification(title, message):
    try:
        from plyer import notification
        import threading
        def _notify():
            notification.notify(
                title=title,
                message=message,
                app_name="GitSync",
                app_icon="ui/assets/logo.ico" if os.path.exists("ui/assets/logo.ico") else None,
                timeout=5
            )
        threading.Thread(target=_notify, daemon=True).start()
    except:
        pass

class GitSyncHandler(FileSystemEventHandler):
    def __init__(self, workspace_path, github_service, ai_service, sync_manager):
        self.workspace_path = workspace_path
        self.gh = github_service
        self.ai = ai_service
        self.sm = sync_manager
        self._changed_repos = set()
        self._timer = None
        self._lock = threading.Lock()
        
    def _schedule_sync(self, repo_path):
        with self._lock:
            self._changed_repos.add(repo_path)
            if self._timer:
                self._timer.cancel()
            interval = float(load_settings().get('sync_interval', 3))
            self._timer = threading.Timer(interval, self._perform_sync)
            self._timer.start()

    def _perform_sync(self):
        with self._lock:
            repos_to_sync = list(self._changed_repos)
            self._changed_repos.clear()
            
        for repo_path in repos_to_sync:
            try:
                self.sm.sync_local_repo(repo_path)
            except Exception as e:
                log_action("ERROR", f"Sync failed for {repo_path}: {e}", "Watcher")
            
    def _get_repo_root(self, path):
        current = path
        while current and current.startswith(self.workspace_path):
            if os.path.isdir(os.path.join(current, ".git")):
                return current
            parent = os.path.dirname(current)
            if parent == current:
                break
            current = parent
        
        # New folder directly under workspace
        if os.path.dirname(path) == self.workspace_path and os.path.isdir(path):
            return path
        return None

    def on_any_event(self, event):
        # Handle safe deletion system
        if event.is_directory and event.event_type == 'deleted':
            repo_mapping = get_repo_mapping_by_path(event.src_path)
            if repo_mapping:
                log_action("WARNING", f"Repository folder deleted: {event.src_path}", "Watcher")
                add_pending_deletion(repo_mapping["repo_name"], "Folder deleted from workspace")
                return

        # Ignore .git and temp files changes to prevent loops
        if ".git" in event.src_path or "__pycache__" in event.src_path:
            return

        repo_root = self._get_repo_root(event.src_path)
        if repo_root:
            self._schedule_sync(repo_root)


class SyncManager:
    def __init__(self):
        self.observers = {}
        self.gh = GitHubService()
        self.ai = AIService()
        self.lock = threading.Lock()
        self.stop_scheduler = threading.Event()
        
    def start_watching(self):
        workspaces = get_workspaces()
        for ws in workspaces:
            path = ws["path"]
            if not ws["is_paused"] and os.path.exists(path):
                self._start_observer(path)

        # Full sync on startup
        threading.Thread(target=self.full_startup_sync, daemon=True).start()
        threading.Thread(target=self.release_scheduler_loop, daemon=True).start()
        
    def stop_watching(self):
        self.stop_scheduler.set()
        with self.lock:
            for obs in self.observers.values():
                obs.stop()
            for obs in self.observers.values():
                obs.join()
            self.observers.clear()
            
    def update_workspaces(self):
        with self.lock:
            workspaces = {ws["path"]: ws for ws in get_workspaces()}
            current_paths = set(self.observers.keys())
            db_paths = set(workspaces.keys())
            
            for path in current_paths:
                if path not in db_paths or workspaces[path]["is_paused"]:
                    self.observers[path].stop()
                    self.observers[path].join()
                    del self.observers[path]
                    
            for path in db_paths:
                if path not in current_paths and not workspaces[path]["is_paused"] and os.path.exists(path):
                    self._start_observer(path)

    def _start_observer(self, path):
        handler = GitSyncHandler(path, self.gh, self.ai, self)
        observer = Observer()
        observer.schedule(handler, path, recursive=True)
        observer.start()
        self.observers[path] = observer
        log_action("INFO", f"Started watching workspace: {path}", "Watcher")

    def release_scheduler_loop(self):
        from backend.database import get_scheduled_uploads, remove_scheduled_upload
        import shutil
        from datetime import datetime
        log_action("INFO", "Started release scheduler loop", "Scheduler")
        while not self.stop_scheduler.is_set():
            try:
                uploads = get_scheduled_uploads()
                now = datetime.now()
                for u in uploads:
                    try: sched_time = datetime.fromisoformat(u["scheduled_at"])
                    except: continue
                        
                    if sched_time <= now:
                        staging_path = u["staging_path"]
                        target_ws = u["target_workspace"]
                        rel_path = u["relative_target_path"]
                        
                        # Use os.path.normpath to safely join the relative path
                        target_full = os.path.normpath(os.path.join(target_ws, rel_path, u["file_name"]))
                        
                        target_dir = os.path.dirname(target_full)
                        os.makedirs(target_dir, exist_ok=True)
                        
                        if os.path.exists(staging_path):
                            shutil.copy2(staging_path, target_full)
                            try: os.remove(staging_path)
                            except: pass
                            log_action("INFO", f"Released scheduled upload: {u['file_name']} into {target_ws}", "Scheduler")
                        else:
                            log_action("WARNING", f"Scheduled file missing from staging: {staging_path}", "Scheduler")
                            
                        remove_scheduled_upload(u["id"])
            except Exception as e:
                log_action("ERROR", f"Scheduler loop error: {e}", "Scheduler")
            
            self.stop_scheduler.wait(10.0)


    def sync_local_repo(self, local_path):
        if not os.path.exists(local_path):
            log_action("WARNING", f"Path no longer exists, skipping sync: {local_path}", "Watcher")
            return
            
        repo_name = os.path.basename(local_path)
        mapping = get_repo_mapping_by_path(local_path)
        
        if not mapping:
            existing_name_mapping = get_repo_mapping_by_name(repo_name)
            if existing_name_mapping:
                log_action("DUPLICATE_REPO", f"Same name repo is already existed: {repo_name}", "Watcher")
                send_desktop_notification("GitSync Warning", f"A repo named '{repo_name}' already exists.")
                return

            log_action("INFO", f"Detected new untracked local folder: {repo_name}", "Watcher")
            
            # Determine visibility from workspace
            ws_visibility = "Private"
            ws_dir = os.path.dirname(local_path)
            for ws in get_workspaces():
                if ws["path"] == ws_dir:
                    ws_visibility = ws["visibility"]
                    break
            
            is_private = (ws_visibility.lower() == "private")
            github_repo = self.gh.create_repo(repo_name, private=is_private)
            if not github_repo:
                log_action("ERROR", f"Failed to create GitHub repo for {repo_name}", "Watcher")
                return
            
            self._ensure_project_files(local_path)
            self.gh.init_and_push(local_path, github_repo.clone_url)
            add_repo_mapping(repo_name, local_path, github_repo.full_name)
            return

        try:
            repo = git.Repo(local_path)
            self._ensure_project_files(local_path)
            
            if not repo.is_dirty(untracked_files=True):
                return

            diff = repo.git.diff("HEAD") if len(repo.heads) > 0 else repo.git.diff()
            if not diff and repo.untracked_files:
                diff = "New untracked files added: " + ", ".join(repo.untracked_files[:5])

            commit_msg = self.ai.generate_commit_message(diff)
            
            repo.git.add(A=True)
            repo.index.commit(commit_msg)
            
            origin = repo.remotes.origin
            origin.push()
            log_action("INFO", f"Pushed updates to {repo_name}: {commit_msg}", "Watcher")
            
            # Clear any previously resolved conflicts if successful
            from backend.database import remove_conflict
            remove_conflict(repo_name)
            
        except Exception as e:
            err_msg = str(e)
            log_action("ERROR", f"Sync process failed for {local_path}: {err_msg}", "Watcher")
            
            # Prevent annoying users heavily, only notify on fatal push/auth/conflict errors
            if "fetch first" in err_msg or "non-fast-forward" in err_msg or "conflict" in err_msg.lower():
                from backend.database import add_conflict
                add_conflict(repo_name, local_path, "Remote changes conflict with your local files.")
                send_desktop_notification("GitSync Conflict", f"Push rejected for {repo_name}. Please resolve in dashboard.")
            elif "403" in err_msg or "401" in err_msg:
                send_desktop_notification("GitSync Error", f"Sync auth failed for {repo_name}!")
            
    def _ensure_project_files(self, local_path):
        files = os.listdir(local_path)
        
        lang = "python"
        if any(f.endswith(".js") or f.endswith(".ts") or f == "package.json" for f in files):
            lang = "node"
                
        if ".gitignore" not in files:
            content = self.ai.generate_gitignore(lang)
            if content:
                with open(os.path.join(local_path, ".gitignore"), "w", encoding="utf-8") as f:
                    f.write(content)

    def full_startup_sync(self):
        log_action("INFO", "Running startup full sync...", "Watcher")
        try:
            remote_repos = self.gh.get_user_repos()
            if not remote_repos:
                return
                
            remote_repo_names = {r.name: r for r in remote_repos}
            
            workspaces = [ws["path"] for ws in get_workspaces() if not ws["is_paused"]]
            if not workspaces:
                return
                
            primary_ws = workspaces[0]
            
            for name, r in remote_repo_names.items():
                mapping = get_repo_mapping_by_name(name)
                if not mapping:
                    local_path = os.path.join(primary_ws, name)
                    if not os.path.exists(local_path):
                        success = self.gh.clone_repo(r.clone_url, local_path)
                        if success:
                            add_repo_mapping(name, local_path, r.full_name)
        except Exception as e:
            log_action("ERROR", f"Failed full startup sync: {e}", "Watcher")
