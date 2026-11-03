package config

import "time"

// AppConfig is the root persisted configuration.
type AppConfig struct {
	Version     int          `json:"version"`
	Accounts    []Account    `json:"accounts"`
	Repos       []RepoConfig `json:"repos"`
	Preferences Preferences  `json:"preferences"`
}

// Account represents a GitHub identity (PAT stored encrypted).
type Account struct {
	ID           string    `json:"id"`
	Label        string    `json:"label"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	EncryptedPAT string    `json:"encrypted_pat"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// RepoConfig is the configuration for a single watched folder.
type RepoConfig struct {
	ID             string    `json:"id"`
	LocalPath      string    `json:"local_path"`
	RemoteName     string    `json:"remote_name"`
	AccountID      string    `json:"account_id"`
	Visibility     string    `json:"visibility"` // "public" | "private"
	Enabled        bool      `json:"enabled"`
	Branch         string    `json:"branch"` // default: "main"
	CommitMsg      string    `json:"commit_msg"`
	BackupDir      string    `json:"backup_dir"`
	RemoteVerified bool      `json:"remote_verified"` // cached verification from GitHub REST API
	LastRemoteCheck time.Time `json:"last_remote_check,omitempty"`
	LastSyncAt     time.Time `json:"last_sync_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// Preferences holds global application settings.
type Preferences struct {
	GlobalPause          bool   `json:"global_pause"`
	DebounceSec          int    `json:"debounce_sec"`      // default: 30
	MaxDebounceSec       int    `json:"max_debounce_sec"`  // default: 120 (trailing cap)
	BackupsEnabled       bool   `json:"backups_enabled"`
	BackupDir            string `json:"backup_dir"`
	LogMaxSizeMB         int    `json:"log_max_size_mb"`
	LogMaxFiles          int    `json:"log_max_files"`
	StartMinimized       bool   `json:"start_minimized"`
	LaunchOnBoot         bool   `json:"launch_on_boot"`
	Theme                string `json:"theme"` // "dark" | "light" | "system"
	GitPath              string `json:"git_path,omitempty"`
	ExcludeLargeFiles    bool   `json:"exclude_large_files"`    // guard >95MB files
	LargeFileThresholdMB int    `json:"large_file_threshold_mb"`// default: 95
	ShowNotifications    bool   `json:"show_notifications"`
	AIProvider           string `json:"ai_provider,omitempty"`  // "auto" | "gemini" | "openai" | "github"
	AIApiKey             string `json:"ai_api_key,omitempty"`
	AIModel              string `json:"ai_model,omitempty"`
	AIBaseURL            string `json:"ai_base_url,omitempty"`
}
