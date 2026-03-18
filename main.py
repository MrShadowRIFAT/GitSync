import os
import sys
import ctypes
import threading
import multiprocessing
import webbrowser
import socket
import time
import asyncio
import uvicorn
import pystray
from pystray import MenuItem as item
from PIL import Image, ImageDraw

# Step 1: Guarantee base directories
def setup_environment():
    base_dir = r"C:\Program Files\GitSync"
    try:
        os.makedirs(base_dir, exist_ok=True)
        test_file = os.path.join(base_dir, ".write_test")
        with open(test_file, 'w') as f:
            f.write("test")
        os.remove(test_file)
    except (PermissionError, OSError):
        # Fallback to AppData if not running as admin
        base_dir = os.path.join(os.environ.get("LOCALAPPDATA", ""), "GitSync")
        os.makedirs(base_dir, exist_ok=True)
        
    for d in ["config", "data", "logs", "ui"]:
        os.makedirs(os.path.join(base_dir, d), exist_ok=True)

    # Set as environment variable so internal modules use it
    os.environ["GITSYNC_HOME"] = base_dir
    return base_dir

base_dir = setup_environment()

# Redirect stdout and stderr to prevent crashes in PyInstaller --noconsole builds
log_file_path = os.path.join(base_dir, "logs", "app.log")
try:
    sys.stdout = open(log_file_path, "a", encoding="utf-8")
    sys.stderr = open(log_file_path, "a", encoding="utf-8")
except Exception:
    pass

def find_free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(('', 0))
        return s.getsockname()[1]

server_port = find_free_port()
server_url = f"http://127.0.0.1:{server_port}/"

# Pass the port to the backend seamlessly
os.environ["GITSYNC_PORT"] = str(server_port)

# Now we can safely import our backend modules which might rely on the environment variables
from backend.api import app
from backend.database import get_workspaces, update_workspace_status
from backend.api import sync_manager




def open_dashboard():
    webbrowser.open(server_url)

def pause_sync():
    try:
        workspaces = get_workspaces()
        for ws in workspaces:
            update_workspace_status(ws["path"], True)
        sync_manager.update_workspaces()
    except Exception as e:
        print("Pause sync exception:", e)

def resume_sync():
    try:
        workspaces = get_workspaces()
        for ws in workspaces:
            update_workspace_status(ws["path"], False)
        sync_manager.update_workspaces()
    except Exception as e:
        print("Resume sync exception:", e)

def exit_app(icon, item):
    icon.stop()
    os._exit(0) # Force exit all threads immediately

def create_image():
    image = Image.new('RGB', (64, 64), color=(16, 25, 38))
    draw = ImageDraw.Draw(image)
    draw.ellipse([12, 12, 52, 52], fill=(59, 130, 246))
    return image

def run_server():
    config = uvicorn.Config(app, host="127.0.0.1", port=server_port, log_level="warning")
    server = uvicorn.Server(config)
    asyncio.run(server.serve())

def main():
    multiprocessing.freeze_support()
    
    # Single instance lock using Windows Mutex
    mutex_name = "GitSync_SingleInstance_Mutex_1A2B3C"
    mutex = ctypes.windll.kernel32.CreateMutexW(None, False, mutex_name)
    last_error = ctypes.windll.kernel32.GetLastError()
    
    if last_error == 183: # ERROR_ALREADY_EXISTS
        print("GitSync is already running. Exiting.")
        sys.exit(0)
    
    # Keep reference to mutex so it's not garbage collected
    _keep_mutex_alive = mutex
    
    # Start server in background thread
    t = threading.Thread(target=run_server, daemon=True)
    t.start()
    
    # Setup tray
    menu = pystray.Menu(
        item('Open Dashboard', open_dashboard),
        item('Pause Sync', pause_sync),
        item('Resume Sync', resume_sync),
        pystray.Menu.SEPARATOR,
        item('Exit GitSync', exit_app)
    )
    
    icon = pystray.Icon("GitSync", create_image(), "GitSync", menu)
    
    # Blocking call for main thread
    icon.run()

if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--browse-folder":
        import tkinter as tk
        from tkinter import filedialog
        
        output_file = None
        if len(sys.argv) > 2:
            output_file = sys.argv[2]
            
        root = tk.Tk()
        root.withdraw()
        root.attributes('-topmost', True)
        path = filedialog.askdirectory(title="Select Workspace Folder")
        root.destroy()
        
        if output_file:
            with open(output_file, "w", encoding="utf-8") as f:
                f.write(path if path else "")
        elif path:
            print(path)
            
        sys.exit(0)
    main()
