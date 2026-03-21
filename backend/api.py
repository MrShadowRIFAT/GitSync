import os
import subprocess
import webbrowser
import threading
from fastapi import FastAPI, HTTPException, Request
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
from backend.config import load_settings, save_settings
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
    threading.Thread(target=sync_manager.full_startup_sync, daemon=True).start()
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
    success = sync_manager.gh.rename_repo(data.old_name, data.new_name)
    if success:
        update_repo_name(data.old_name, data.new_name)
        return {"success": True}
    raise HTTPException(status_code=500, detail="Failed to rename repository")

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
    total_size = 0
    for ws in workspaces_list:
        path = ws["path"]
        if _os.path.exists(path):
            for dirpath, dirnames, filenames in _os.walk(path):
                for f in filenames:
                    try:
                        total_size += _os.path.getsize(_os.path.join(dirpath, f))
                    except OSError:
                        pass
    return {
        "workspace_count": len(workspaces_list),
        "repo_count": len(repos),
        "total_size_bytes": total_size,
        "total_size_mb": round(total_size / (1024 * 1024), 1),
        "total_size_display": f"{round(total_size / (1024*1024*1024), 2)} GB" if total_size > 1024*1024*1024 else f"{round(total_size / (1024*1024), 1)} MB"
    }

# --- Static UI Serve ---
import sys
if getattr(sys, 'frozen', False):
    ui_path = os.path.join(sys._MEIPASS, "ui")
else:
    ui_path = os.path.join(os.path.dirname(os.path.dirname(__file__)), "ui")

if os.path.exists(ui_path):
    app.mount("/static", StaticFiles(directory=ui_path), name="static")

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
