// Shared TypeScript types for GitSync frontend

export interface Account {
  id: string;
  label: string;
  username: string;
  email: string;
  encrypted_pat: string;
  avatar_url?: string;
  created_at?: string;
}

export interface RateLimitInfo {
  limit: number;
  remaining: number;
  reset_at: string;
  used: number;
}

export interface RepoConfig {
  id: string;
  local_path: string;
  remote_name: string;
  account_id: string;
  visibility: 'public' | 'private';
  enabled: boolean;
  branch: string;
  commit_msg: string;
  backup_dir: string;
}

export interface Preferences {
  global_pause: boolean;
  debounce_sec: number;
  max_debounce_sec?: number;
  backups_enabled: boolean;
  backup_dir: string;
  log_max_size_mb: number;
  log_max_files: number;
  start_minimized: boolean;
  launch_on_boot?: boolean;
  theme: 'dark' | 'light' | 'system';
  git_path?: string;
  exclude_large_files?: boolean;
  large_file_threshold_mb?: number;
  show_notifications?: boolean;
  ai_provider?: string;
  ai_api_key?: string;
  ai_model?: string;
  ai_base_url?: string;
}

export interface AppConfig {
  version: number;
  accounts: Account[];
  repos: RepoConfig[];
  preferences: Preferences;
}

export type RepoState = 'idle' | 'debouncing' | 'syncing' | 'offline' | 'error' | 'conflict' | 'paused';

export interface RepoStatus {
  repo_id: string;
  local_path: string;
  remote_name: string;
  state: RepoState;
  last_sync_at: string;
  last_error: string;
  pending_files: number;
}

export type LogLevel = 'INFO' | 'WARN' | 'ERROR' | 'DEBUG';

export interface LogEntry {
  level: LogLevel;
  component: string;
  message: string;
  timestamp: string;
}

export type ActiveView = 'dashboard' | 'logs' | 'settings';

export interface ToastMessage {
  id: string;
  type: 'success' | 'info' | 'warning' | 'error' | 'conflict' | 'backup';
  title: string;
  message: string;
  repoName?: string;
  timestamp?: string;
  duration?: number;
}

export interface UpdateInfo {
  current_version: string;
  latest_version: string;
  has_update: boolean;
  release_name: string;
  release_url: string;
  download_url: string;
  published_at: string;
  release_notes: string;
  last_checked: string;
  error: string;
}
