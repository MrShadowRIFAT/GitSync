import os
import time
import threading
from watchdog.observers import Observer
from watchdog.events import FileSystemEventHandler
from backend.database import (
    get_workspaces, get_repo_mapping_by_path, get_repo_mapping_by_name, add_repo_mapping, 
    remove_repo_mapping, add_pending_deletion, log_action
)
from backend.github_service import GitHubService
from backend.ai_service import AIService
import git

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
            # Wait 3 seconds of inactivity before syncing
            self._timer = threading.Timer(3.0, self._perform_sync)
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
        
    def start_watching(self):
        workspaces = get_workspaces()
        for ws in workspaces:
            path = ws["path"]
            if not ws["is_paused"] and os.path.exists(path):
                self._start_observer(path)

        # Full sync on startup
        threading.Thread(target=self.full_startup_sync, daemon=True).start()
        
    def stop_watching(self):
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

    def sync_local_repo(self, local_path):
        if not os.path.exists(local_path):
            log_action("WARNING", f"Path no longer exists, skipping sync: {local_path}", "Watcher")
            return
            
        repo_name = os.path.basename(local_path)
        mapping = get_repo_mapping_by_path(local_path)
        
        if not mapping:
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
        except Exception as e:
            log_action("ERROR", f"Sync process failed for {local_path}: {e}", "Watcher")
            
    def _ensure_project_files(self, local_path):
        files = os.listdir(local_path)
        
        lang = "python"
        if any(f.endswith(".js") or f.endswith(".ts") or f == "package.json" for f in files):
            lang = "node"
            
        if "README.md" not in [f.upper() for f in files]:
            content = self.ai.generate_readme(lang, os.path.basename(local_path))
            with open(os.path.join(local_path, "README.md"), "w", encoding="utf-8") as f:
                f.write(content)
                
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
