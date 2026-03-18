import sqlite3
import os

def get_data_dir():
    home = os.environ.get("GITSYNC_HOME")
    if home:
        return os.path.join(home, "data")
    appdata = os.environ.get('LOCALAPPDATA') or os.environ.get('APPDATA') or os.path.expanduser("~")
    return os.path.join(appdata, "GitSync", "data")

DB_DIR = get_data_dir()
DB_PATH = os.path.join(DB_DIR, "gitsync.db")

def get_connection():
    os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)
    conn = sqlite3.connect(DB_PATH, check_same_thread=False)
    conn.row_factory = sqlite3.Row
    return conn

def init_db():
    conn = get_connection()
    cursor = conn.cursor()
    
    # Repo Mapping
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS repo_mapping (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            repo_name TEXT UNIQUE,
            local_path TEXT,
            github_repo TEXT,
            created_at TEXT
        )
    ''')
    
    # Pending Deletions
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS pending_deletions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            repo_name TEXT UNIQUE,
            detected_time TEXT,
            reason TEXT
        )
    ''')
    
    # Workspaces
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS workspaces (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            path TEXT UNIQUE,
            visibility TEXT DEFAULT 'Private',
            is_paused INTEGER DEFAULT 0
        )
    ''')

    # Logs
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS action_logs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp TEXT,
            level TEXT,
            message TEXT
        )
    ''')
    
    conn.commit()
    conn.close()

def log_action(level, message, source="system"):
    try:
        conn = get_connection()
        c = conn.cursor()
        from datetime import datetime
        c.execute("INSERT INTO action_logs (timestamp, level, message) VALUES (?, ?, ?)",
                  (datetime.now().isoformat(), level, f"[{source}] {message}"))
        conn.commit()
        conn.close()
    except:
        pass

def get_logs(limit=100):
    conn = get_connection()
    c = conn.cursor()
    c.execute("SELECT * FROM action_logs ORDER BY id DESC LIMIT ?", (limit,))
    rows = c.fetchall()
    conn.close()
    return [dict(r) for r in rows]

# --- Workspaces ---
def add_workspace(path, visibility="Private"):
    conn = get_connection()
    c = conn.cursor()
    try:
        c.execute("INSERT INTO workspaces (path, visibility, is_paused) VALUES (?, ?, ?)", (path, visibility, 0))
        conn.commit()
        return True
    except sqlite3.IntegrityError:
        return False
    finally:
        conn.close()

def remove_workspace(path):
    conn = get_connection()
    c = conn.cursor()
    c.execute("DELETE FROM workspaces WHERE path = ?", (path,))
    conn.commit()
    conn.close()

def get_workspaces():
    conn = get_connection()
    c = conn.cursor()
    c.execute("SELECT * FROM workspaces")
    rows = c.fetchall()
    conn.close()
    return [dict(r) for r in rows]

def update_workspace_status(path, is_paused):
    conn = get_connection()
    c = conn.cursor()
    c.execute("UPDATE workspaces SET is_paused = ? WHERE path = ?", (1 if is_paused else 0, path))
    conn.commit()
    conn.close()

# --- Repository Mapping ---
def get_repo_mappings():
    conn = get_connection()
    c = conn.cursor()
    c.execute("SELECT * FROM repo_mapping")
    rows = c.fetchall()
    conn.close()
    return [dict(r) for r in rows]

def get_repo_mapping_by_path(local_path):
    conn = get_connection()
    c = conn.cursor()
    c.execute("SELECT * FROM repo_mapping WHERE local_path = ?", (local_path,))
    row = c.fetchone()
    conn.close()
    return dict(row) if row else None

def get_repo_mapping_by_name(repo_name):
    conn = get_connection()
    c = conn.cursor()
    c.execute("SELECT * FROM repo_mapping WHERE repo_name = ?", (repo_name,))
    row = c.fetchone()
    conn.close()
    return dict(row) if row else None

def add_repo_mapping(repo_name, local_path, github_repo):
    from datetime import datetime
    conn = get_connection()
    c = conn.cursor()
    try:
        c.execute("INSERT INTO repo_mapping (repo_name, local_path, github_repo, created_at) VALUES (?, ?, ?, ?)",
                  (repo_name, local_path, github_repo, datetime.now().isoformat()))
        conn.commit()
    except sqlite3.IntegrityError:
        # If exists, update local path in case it moved
        c.execute("UPDATE repo_mapping SET local_path = ?, github_repo = ? WHERE repo_name = ?", (local_path, github_repo, repo_name))
        conn.commit()
    finally:
        conn.close()

def remove_repo_mapping(repo_name):
    conn = get_connection()
    c = conn.cursor()
    c.execute("DELETE FROM repo_mapping WHERE repo_name = ?", (repo_name,))
    conn.commit()
    conn.close()

# --- Pending Deletions ---
def get_pending_deletions():
    conn = get_connection()
    c = conn.cursor()
    c.execute("SELECT * FROM pending_deletions")
    rows = c.fetchall()
    conn.close()
    return [dict(r) for r in rows]

def add_pending_deletion(repo_name, reason):
    from datetime import datetime
    conn = get_connection()
    c = conn.cursor()
    try:
        c.execute("INSERT INTO pending_deletions (repo_name, detected_time, reason) VALUES (?, ?, ?)",
                  (repo_name, datetime.now().isoformat(), reason))
        conn.commit()
    except sqlite3.IntegrityError:
        pass
    finally:
        conn.close()

def remove_pending_deletion(repo_name):
    conn = get_connection()
    c = conn.cursor()
    c.execute("DELETE FROM pending_deletions WHERE repo_name = ?", (repo_name,))
    conn.commit()
    conn.close()

def clear_logs():
    conn = get_connection()
    c = conn.cursor()
    c.execute("DELETE FROM action_logs")
    conn.commit()
    conn.close()

def clear_cache():
    conn = get_connection()
    c = conn.cursor()
    c.execute("DELETE FROM repo_mapping")
    c.execute("DELETE FROM pending_deletions")
    c.execute("DELETE FROM action_logs")
    conn.commit()
    conn.close()

def update_repo_name(old_name, new_name):
    conn = get_connection()
    c = conn.cursor()
    c.execute("UPDATE repo_mapping SET repo_name = ?, github_repo = REPLACE(github_repo, ?, ?) WHERE repo_name = ?",
              (new_name, old_name, new_name, old_name))
    conn.commit()
    conn.close()
