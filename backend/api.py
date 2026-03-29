import os
import subprocess
import webbrowser
import threading
from fastapi import FastAPI, HTTPException, Request, UploadFile, File, Form
from fastapi.staticfiles import StaticFiles
from fastapi.responses import FileResponse
from fastapi.middleware.cors import CORSMiddleware
from backend.database import (
    get_workspaces, add_workspace, remove_workspace, update_workspace_status,
    get_repo_mappings, get_pending_deletions, remove_pending_deletion, get_logs,
    log_action, init_db, clear_logs, clear_cache, update_repo_name
)
from pydantic import BaseModel
from backend.watcher import SyncManager
from backend.config import load_settings, save_settings, APP_VERSION, GITHUB_REPO
from backend.github_service import GitHubService


app = FastAPI()

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

# Initialize Sync Manager
sync_manager = SyncManager()

# --- Models ---
class WorkspaceModel(BaseModel):
    path: str
    visibility: str = "Private"

class SettingsModel(BaseModel):
    github_token: str = ""
    openai_key: str = ""
    gemini_key: str = ""
    sync_interval: int = 3
    readme_template: str = ""
    auto_update: bool = True
    custom_port: int = 0

class DeleteRepoModel(BaseModel):
    repo_name: str

class SyncRepoModel(BaseModel):
    local_path: str

class PauseWorkspaceModel(BaseModel):
    path: str

class RenameRepoModel(BaseModel):
    old_name: str
    new_name: str

class VisibilityModel(BaseModel):
    repo_name: str
    private: bool

class ResolveConflictModel(BaseModel):
    repo_name: str
    action: str

# --- Endpoints ---

def open_browser():
    port = os.environ.get("GITSYNC_PORT")
    if port:
        webbrowser.open(f"http://127.0.0.1:{port}/")

@app.on_event("startup")
def startup_event():
    init_db()
    sync_manager.start_watching()
    log_action("INFO", "FastAPI server started", "API")
    # Open the browser immediately now that FastAPI is fully bound
    threading.Timer(0.1, open_browser).start()

@app.on_event("shutdown")
def shutdown_event():
    sync_manager.stop_watching()
    log_action("INFO", "FastAPI server stopped", "API")

@app.get("/api/workspaces")
def api_get_workspaces():
    return get_workspaces()

@app.get("/api/browse-folder")
def api_browse_folder():
    try:
        import sys
        import tempfile
        import os
        
        fd, tmp_path = tempfile.mkstemp(suffix=".txt")
        os.close(fd)
        
        cmd = [sys.executable]
        if not getattr(sys, 'frozen', False):
            cmd.append("main.py")
        cmd.extend(["--browse-folder", tmp_path])

        CREATE_NO_WINDOW = 0x08000000
        subprocess.run(
            cmd,
            creationflags=CREATE_NO_WINDOW
        )
        
        with open(tmp_path, "r", encoding="utf-8") as f:
            path = f.read().strip()
            
        try:
            os.remove(tmp_path)
        except OSError:
            pass
            
        return {"path": path}
    except Exception as e:
        log_action("ERROR", f"Failed to open native picker: {e}", "API")
        raise HTTPException(status_code=500, detail="Could not open browser dialog")

@app.post("/api/open-dropzone")
def api_open_dropzone():
    import threading
    def _open_dz():
        import tkinter as tk
        import windnd
        import os
        from backend.database import add_workspace
        from backend.core import sync_manager
        
        root = tk.Tk()
        root.title("GitSync Drop Zone")
        root.attributes('-topmost', True)
        root.geometry("340x160")
        root.configure(bg="#1E1E2E")
        
        lbl = tk.Label(root, text="Drop Workspace Folders Here", fg="#A6ACCD", bg="#1E1E2E", font=("Segoe UI", 12, "bold"))
        lbl.pack(expand=True, fill=tk.BOTH)
        
        def handle_drop(files):
            added: int = 0
            for item in files:
                try: path = item.decode('mbcs')
                except: path = item.decode('utf-8', 'ignore')
                if os.path.isdir(path):
                    if add_workspace(path, 'Private'):
                        sync_manager.update_workspaces()
                        log_action("INFO", f"Tracked workspace via DropZone: {path}", "Watcher")
                        added += 1  # pyre-ignore
            if added > 0:
                root.destroy()
                
        windnd.hook_dropfiles(root, func=handle_drop)
        root.mainloop()
        
    threading.Thread(target=_open_dz, daemon=True).start()
    return {"success": True}

@app.post("/api/add-workspace")
def api_add_workspace(ws: WorkspaceModel):
    if not os.path.exists(ws.path):
        raise HTTPException(status_code=400, detail="Path does not exist on disk")
    if add_workspace(ws.path, ws.visibility):
        sync_manager.update_workspaces()
        log_action("INFO", f"Added workspace: {ws.path}", "API")
        return {"success": True}
    raise HTTPException(status_code=400, detail="Workspace already exists")

@app.post("/api/remove-workspace")
def api_remove_workspace(ws: WorkspaceModel):
    remove_workspace(ws.path)
    sync_manager.update_workspaces()
    log_action("INFO", f"Removed workspace: {ws.path}", "API")
    return {"success": True}

@app.post("/api/pause-workspace")
def api_pause_workspace(ws: PauseWorkspaceModel):
    update_workspace_status(ws.path, True)
    sync_manager.update_workspaces()
    log_action("INFO", f"Paused workspace: {ws.path}", "API")
    return {"success": True}

@app.post("/api/resume-workspace")
def api_resume_workspace(ws: PauseWorkspaceModel):
    update_workspace_status(ws.path, False)
    sync_manager.update_workspaces()
    log_action("INFO", f"Resumed workspace: {ws.path}", "API")
    return {"success": True}

@app.get("/api/repositories")
def api_get_repositories():
    return get_repo_mappings()

@app.get("/api/pending-deletions")
def api_get_pending_deletions():
    return get_pending_deletions()

@app.get("/api/conflicts")
def api_get_conflicts():
    from backend.database import get_conflicts
    return get_conflicts()

@app.get("/api/logs")
def api_get_logs():
    return get_logs(100)

@app.get("/api/settings")
def api_get_settings():
    return load_settings()

@app.post("/api/settings")
def api_save_settings(settings: SettingsModel):
    current = load_settings()
    current["github_token"] = settings.github_token
    current["openai_key"] = settings.openai_key
    current["gemini_key"] = settings.gemini_key
    current["sync_interval"] = settings.sync_interval
    current["readme_template"] = settings.readme_template
    current["auto_update"] = settings.auto_update
    current["custom_port"] = settings.custom_port
    save_settings(current)
    
    sync_manager.gh.refresh_token()
    sync_manager.ai.refresh_keys()
    
    github = GitHubService()
    profile = github.get_user_profile()
    log_action("INFO", "Settings updated", "API")
    return {"success": True, "profile": profile}

@app.post("/api/sync")
def api_sync(data: SyncRepoModel):
    import threading
    threading.Thread(target=sync_manager.sync_local_repo, args=(data.local_path,), daemon=True).start()
    return {"success": True}

@app.post("/api/sync/all")
def api_sync_all():
    import threading
    def _sync():
        sync_manager.full_startup_sync()
        try:
            from backend.watcher import send_desktop_notification
            send_desktop_notification("GitSync Desktop", "Bulk repository sync completed successfully!")
        except:
            pass
    threading.Thread(target=_sync, daemon=True).start()
    return {"success": True}

@app.post("/api/delete-repo")
def api_delete_repo(data: DeleteRepoModel):
    success = sync_manager.gh.delete_repo(data.repo_name)
    if success:
        remove_pending_deletion(data.repo_name)
        log_action("INFO", f"Confirmed safe deletion for {data.repo_name}", "API")
        return {"success": True}
    raise HTTPException(status_code=500, detail="Failed to delete from GitHub")

@app.post("/api/cancel-deletion")
def api_cancel_deletion(data: DeleteRepoModel):
    remove_pending_deletion(data.repo_name)
    log_action("INFO", f"Cancelled deletion for {data.repo_name}", "API")
    return {"success": True}

@app.post("/api/resolve-conflict")
def api_resolve_conflict(data: ResolveConflictModel):
    from backend.database import get_repo_mapping_by_name, remove_conflict
    mapping = get_repo_mapping_by_name(data.repo_name)
    if not mapping:
        raise HTTPException(status_code=404, detail="Repo not found")
        
    local_path = mapping["local_path"]
    try:
        import git
        repo = git.Repo(local_path)
        origin = repo.remotes.origin
        
        if data.action == "force_push":
            origin.push(force=True)
            log_action("INFO", f"Force pushed updates to resolve conflict: {data.repo_name}", "API")
        elif data.action == "pull":
            origin.fetch('--all')
            branch = repo.active_branch.name
            repo.git.reset('--hard', f'origin/{branch}')
            log_action("INFO", f"Overwrote local with remote for {data.repo_name}", "API")
            
        remove_conflict(data.repo_name)
        return {"success": True}
    except Exception as e:
        log_action("ERROR", f"Failed to resolve conflict: {e}", "API")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/api/generate-readme")
def api_generate_readme(data: SyncRepoModel):
    import os as _os
    local_path = data.local_path
    if not _os.path.exists(local_path):
        raise HTTPException(status_code=400, detail="Path does not exist")
    files = _os.listdir(local_path)
    lang = "node" if any(f.endswith((".js", ".ts")) or f == "package.json" for f in files) else "python"
    content = sync_manager.ai.generate_readme(lang, _os.path.basename(local_path))
    readme_path = _os.path.join(local_path, "README.md")
    with open(readme_path, "w", encoding="utf-8") as f:
        f.write(content)
    log_action("INFO", f"Generated README.md for {_os.path.basename(local_path)}", "API")
    return {"success": True}

@app.post("/api/clear-logs")
def api_clear_logs():
    clear_logs()
    log_action("INFO", "Logs cleared by user", "API")
    return {"success": True}

@app.post("/api/clear-cache")
def api_clear_cache():
    clear_cache()
    log_action("INFO", "All cache data cleared by user", "API")
    return {"success": True}

@app.post("/api/rename-repo")
def api_rename_repo(data: RenameRepoModel):
    from backend.database import get_repo_mapping_by_name
    import os
    import platform
    
    mapping = get_repo_mapping_by_name(data.old_name)
    if not mapping:
        raise HTTPException(status_code=404, detail="Repository not found locally")
        
    old_path = mapping["local_path"]
    new_path = os.path.join(os.path.dirname(old_path), data.new_name)
    
    # 1. Rename on GitHub
    success = sync_manager.gh.rename_repo(data.old_name, data.new_name)
    if not success:
        raise HTTPException(status_code=500, detail="Failed to rename repository on GitHub")
        
    # 2. Rename Local Directory
    try:
        if os.path.exists(old_path):
            os.rename(old_path, new_path)
    except Exception as e:
        log_action("ERROR", f"Local rename failed for {old_path}: {e}", "API")
        # Note: Github rename succeeded, but local failed. This might cause sync disconnects.
        
    # 3. Update DB
    update_repo_name(data.old_name, data.new_name, new_path)
    return {"success": True}

@app.post("/api/toggle-visibility")
def api_toggle_visibility(data: VisibilityModel):
    success = sync_manager.gh.toggle_visibility(data.repo_name, data.private)
    if success:
        return {"success": True}
    raise HTTPException(status_code=500, detail="Failed to toggle visibility")

@app.get("/api/stats")
def api_stats():
    import os as _os
    workspaces_list = get_workspaces()
    repos = get_repo_mappings()
    total_size: int = 0
    for ws in workspaces_list:
        path = ws["path"]
        if _os.path.exists(path):
            for dirpath, dirnames, filenames in _os.walk(path):
                for f in filenames:
                    try:
                        total_size += _os.path.getsize(_os.path.join(dirpath, f))  # pyre-ignore
                    except OSError:
                        pass
    return {
        "workspace_count": len(workspaces_list),
        "repo_count": len(repos),
        "total_size_bytes": total_size,
        "total_size_mb": round(float(total_size) / (1024 * 1024), 1),
        "total_size_display": f"{round(float(total_size) / (1024**3), 2)} GB" if total_size > 1024**3 else f"{round(float(total_size) / (1024**2), 1)} MB"
    }

# --- Static UI Serve ---
import sys
@app.post("/api/upload-scheduled")
async def api_upload_scheduled(
    target_workspace: str = Form(...),
    relative_target_path: str = Form(""),
    scheduled_at: str = Form(...),
    files: list[UploadFile] = File(...)
):
    from backend.database import get_data_dir, add_scheduled_upload
    import shutil
    import uuid
    import os
    from datetime import datetime
    
    try:
        datetime.fromisoformat(scheduled_at)
    except:
        raise HTTPException(status_code=400, detail="Invalid ISO format for scheduled_at")
        
    staging_dir = os.path.join(get_data_dir(), "_gitsync_staging")
    os.makedirs(staging_dir, exist_ok=True)
    
    batch_dir = os.path.join(staging_dir, str(uuid.uuid4()))
    os.makedirs(batch_dir, exist_ok=True)
    
    for file in files:
        safe_name = os.path.basename(file.filename)
        out_path = os.path.join(batch_dir, safe_name)
        with open(out_path, "wb") as buffer:
            shutil.copyfileobj(file.file, buffer)
            
        add_scheduled_upload(
            file_name=safe_name,
            staging_path=out_path,
            target_workspace=target_workspace,
            relative_target_path=relative_target_path,
            scheduled_at=scheduled_at
        )
        log_action("INFO", f"Staged {safe_name} for upload at {scheduled_at}", "Scheduler")
        
    return {"success": True, "files_staged": len(files)}

@app.get("/api/scheduled-uploads")
def api_get_scheduled_uploads():
    from backend.database import get_scheduled_uploads
    return get_scheduled_uploads()

@app.post("/api/cancel-scheduled")
def api_cancel_scheduled(data: dict):
    upload_id = data.get("id")
    if not upload_id:
        raise HTTPException(status_code=400, detail="Missing id")
        
    from backend.database import get_scheduled_uploads, remove_scheduled_upload
    import os
    
    uploads = get_scheduled_uploads()
    target = next((u for u in uploads if u["id"] == upload_id), None)
    if target:
        staging_path = target["staging_path"]
        try:
            if os.path.exists(staging_path):
                os.remove(staging_path)
        except OSError:
            pass
        remove_scheduled_upload(upload_id)
        log_action("INFO", f"Cancelled scheduled upload: {target['file_name']}", "Scheduler")
        return {"success": True}
    raise HTTPException(status_code=404, detail="Scheduled upload not found")

if getattr(sys, 'frozen', False):
    ui_path = os.path.join(sys._MEIPASS, "ui")
else:
    ui_path = os.path.join(os.path.dirname(os.path.dirname(__file__)), "ui")

if os.path.exists(ui_path):
    app.mount("/static", StaticFiles(directory=ui_path), name="static")

@app.get("/api/stats")
def api_get_stats():
    from backend.database import get_workspaces, get_repo_mappings
    import os
    workspaces = get_workspaces()
    repos = get_repo_mappings()
    total_size: int = 0
    for ws in workspaces:
        path = ws["path"]
        if os.path.isdir(path):
            for dirpath, _, filenames in os.walk(path):
                if ".git" in dirpath: continue
                # Basic size calculation for workspace files
                for f in filenames:
                    fp = os.path.join(dirpath, f)
                    if not os.path.islink(fp):
                        try: total_size += os.path.getsize(fp)  # pyre-ignore
                        except: pass
    if total_size > 1024**3:
        formatted = f"{round(float(total_size) / (1024**3), 2)} GB"
    else:
        formatted = f"{round(float(total_size) / (1024**2), 1)} MB"
    return {"workspaces": len(workspaces), "repos": len(repos), "total_size_bytes": total_size, "formatted_size": formatted}

@app.get("/api/app-version")
def api_app_version():
    return {"version": APP_VERSION}

@app.get("/api/check-update")
def api_check_update():
    import urllib.request, json as _json
    try:
        url = f"https://api.github.com/repos/{GITHUB_REPO}/releases/latest"
        req = urllib.request.Request(url, headers={"User-Agent": "GitSync-Updater"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = _json.loads(resp.read().decode())
        latest = data.get("tag_name", "").lstrip("v")
        current = APP_VERSION
        import sys
        is_packaged = getattr(sys, 'frozen', False)
        has_update = (latest and latest != current) if is_packaged else False
        download_url = ""
        for asset in data.get("assets", []):
            if asset["name"].lower().endswith(".exe"):
                download_url = asset["browser_download_url"]
                break
        return {
            "has_update": has_update,
            "current_version": current,
            "latest_version": latest,
            "download_url": download_url,
            "release_notes": data.get("body", ""),
            "html_url": data.get("html_url", "")
        }
    except Exception as e:
        return {"has_update": False, "current_version": APP_VERSION, "latest_version": APP_VERSION, "error": str(e)}

@app.post("/api/do-update")
async def api_do_update(request: Request):
    import urllib.request, json as _json
    try:
        body = await request.json()
        download_url = body.get("download_url", "")
        if not download_url:
            raise HTTPException(status_code=400, detail="No download URL provided")
        import sys
        if not getattr(sys, 'frozen', False):
            raise HTTPException(status_code=400, detail="Auto-update is only supported in the packaged .exe version")
            
        exe_path = sys.executable
        update_dir = os.path.join(os.path.dirname(exe_path), "_update")
        os.makedirs(update_dir, exist_ok=True)
        new_exe = os.path.join(update_dir, "GitSync_new.exe")
        
        log_action("INFO", f"Downloading update from {download_url}", "Updater")
        req = urllib.request.Request(download_url, headers={"User-Agent": "GitSync-Updater"})
        with urllib.request.urlopen(req, timeout=120) as resp:
            with open(new_exe, "wb") as f:
                f.write(resp.read())
        
        # Create a batch script to replace exe after process exits
        bat_path = os.path.join(update_dir, "update.bat")
        with open(bat_path, "w") as bat:
            bat.write(f"""@echo off
timeout /t 3 /nobreak >nul
copy /Y "{new_exe}" "{exe_path}"
del "{new_exe}"
start "" "{exe_path}"
del "%~f0"
""")
        
        log_action("INFO", "Update downloaded. Restarting...", "Updater")
        subprocess.Popen(["cmd", "/c", bat_path], creationflags=0x00000008)
        
        # Shutdown the app
        import signal
        os.kill(os.getpid(), signal.SIGTERM)
        
        return {"success": True}
    except HTTPException:
        raise
    except Exception as e:
        log_action("ERROR", f"Update failed: {e}", "Updater")
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/")
def serve_index():
    index_file = os.path.join(ui_path, "index.html")
    if os.path.exists(index_file):
        return FileResponse(index_file)
    return {"error": "UI not found natively.", "path": index_file}

@app.get("/{catchall:path}")
def serve_fallback(catchall: str):
    # Handle SPA routing by returning index.html for unknown paths that don't look like files
    if "." not in catchall:
        index_file = os.path.join(ui_path, "index.html")
        if os.path.exists(index_file):
            return FileResponse(index_file)
    raise HTTPException(status_code=404, detail="Not Found")
