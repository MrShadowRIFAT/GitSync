import os
import json

CONFIG_DIR = r"C:\Program Files\GitSync\config"
if not os.path.exists(CONFIG_DIR) and not os.access(r"C:\Program Files", os.W_OK):
    appdata = os.environ.get('LOCALAPPDATA', os.environ.get('APPDATA'))
    CONFIG_DIR = os.path.join(appdata, "GitSync", "config")

CONFIG_PATH = os.path.join(CONFIG_DIR, "settings.json")

def load_settings():
    if not os.path.exists(CONFIG_PATH):
        return {}
    try:
        with open(CONFIG_PATH, "r") as f:
            return json.load(f)
    except:
        return {}

def save_settings(settings):
    os.makedirs(os.path.dirname(CONFIG_PATH), exist_ok=True)
    with open(CONFIG_PATH, "w") as f:
        json.dump(settings, f, indent=4)
