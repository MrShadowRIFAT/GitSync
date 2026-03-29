import os
import json

APP_VERSION = "2.2.1"
GITHUB_REPO = "MrShadowRIFAT/GitSync"

def get_config_dir():
    home = os.environ.get("GITSYNC_HOME")
    if home:
        return os.path.join(home, "config")
    appdata = os.environ.get('LOCALAPPDATA') or os.environ.get('APPDATA') or os.path.expanduser("~")
    return os.path.join(appdata, "GitSync", "config")

CONFIG_DIR = get_config_dir()
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
