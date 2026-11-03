export namespace config {
	
	export class Account {
	    id: string;
	    label: string;
	    username: string;
	    email: string;
	    encrypted_pat: string;
	    avatar_url?: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Account(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.username = source["username"];
	        this.email = source["email"];
	        this.encrypted_pat = source["encrypted_pat"];
	        this.avatar_url = source["avatar_url"];
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Preferences {
	    global_pause: boolean;
	    debounce_sec: number;
	    max_debounce_sec: number;
	    backups_enabled: boolean;
	    backup_dir: string;
	    log_max_size_mb: number;
	    log_max_files: number;
	    start_minimized: boolean;
	    launch_on_boot: boolean;
	    theme: string;
	    git_path?: string;
	    exclude_large_files: boolean;
	    large_file_threshold_mb: number;
	    show_notifications: boolean;
	    ai_provider?: string;
	    ai_api_key?: string;
	    ai_model?: string;
	    ai_base_url?: string;
	
	    static createFrom(source: any = {}) {
	        return new Preferences(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.global_pause = source["global_pause"];
	        this.debounce_sec = source["debounce_sec"];
	        this.max_debounce_sec = source["max_debounce_sec"];
	        this.backups_enabled = source["backups_enabled"];
	        this.backup_dir = source["backup_dir"];
	        this.log_max_size_mb = source["log_max_size_mb"];
	        this.log_max_files = source["log_max_files"];
	        this.start_minimized = source["start_minimized"];
	        this.launch_on_boot = source["launch_on_boot"];
	        this.theme = source["theme"];
	        this.git_path = source["git_path"];
	        this.exclude_large_files = source["exclude_large_files"];
	        this.large_file_threshold_mb = source["large_file_threshold_mb"];
	        this.show_notifications = source["show_notifications"];
	        this.ai_provider = source["ai_provider"];
	        this.ai_api_key = source["ai_api_key"];
	        this.ai_model = source["ai_model"];
	        this.ai_base_url = source["ai_base_url"];
	    }
	}
	export class RepoConfig {
	    id: string;
	    local_path: string;
	    remote_name: string;
	    account_id: string;
	    visibility: string;
	    enabled: boolean;
	    branch: string;
	    commit_msg: string;
	    backup_dir: string;
	    remote_verified: boolean;
	    // Go type: time
	    last_remote_check?: any;
	    // Go type: time
	    last_sync_at?: any;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new RepoConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.local_path = source["local_path"];
	        this.remote_name = source["remote_name"];
	        this.account_id = source["account_id"];
	        this.visibility = source["visibility"];
	        this.enabled = source["enabled"];
	        this.branch = source["branch"];
	        this.commit_msg = source["commit_msg"];
	        this.backup_dir = source["backup_dir"];
	        this.remote_verified = source["remote_verified"];
	        this.last_remote_check = this.convertValues(source["last_remote_check"], null);
	        this.last_sync_at = this.convertValues(source["last_sync_at"], null);
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AppConfig {
	    version: number;
	    accounts: Account[];
	    repos: RepoConfig[];
	    preferences: Preferences;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.accounts = this.convertValues(source["accounts"], Account);
	        this.repos = this.convertValues(source["repos"], RepoConfig);
	        this.preferences = this.convertValues(source["preferences"], Preferences);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

export namespace logger {
	
	export class Entry {
	    level: string;
	    component: string;
	    message: string;
	    timestamp: string;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.component = source["component"];
	        this.message = source["message"];
	        this.timestamp = source["timestamp"];
	    }
	}

}

export namespace updater {
	
	export class UpdateInfo {
	    current_version: string;
	    latest_version: string;
	    has_update: boolean;
	    release_name: string;
	    release_url: string;
	    download_url: string;
	    published_at: string;
	    release_notes: string;
	    // Go type: time
	    last_checked: any;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current_version = source["current_version"];
	        this.latest_version = source["latest_version"];
	        this.has_update = source["has_update"];
	        this.release_name = source["release_name"];
	        this.release_url = source["release_url"];
	        this.download_url = source["download_url"];
	        this.published_at = source["published_at"];
	        this.release_notes = source["release_notes"];
	        this.last_checked = this.convertValues(source["last_checked"], null);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace watcher {
	
	export class RepoStatus {
	    repo_id: string;
	    local_path: string;
	    remote_name: string;
	    state: string;
	    // Go type: time
	    last_sync_at: any;
	    last_error: string;
	    pending_files: number;
	
	    static createFrom(source: any = {}) {
	        return new RepoStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo_id = source["repo_id"];
	        this.local_path = source["local_path"];
	        this.remote_name = source["remote_name"];
	        this.state = source["state"];
	        this.last_sync_at = this.convertValues(source["last_sync_at"], null);
	        this.last_error = source["last_error"];
	        this.pending_files = source["pending_files"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

