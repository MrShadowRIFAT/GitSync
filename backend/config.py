import os
import json

CONFIG_DIR = r"C:\Program Files\GitSync\config"
if not os.path.exists(CONFIG_DIR):
    CONFIG_DIR = os.path.join(os.path.dirname(os.path.dirname(__file__)), "config")

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
