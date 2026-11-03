package config

import (
	"strings"
	"time"
)

const currentVersion = 3

// migrate upgrades the config schema from older versions.
func migrate(cfg *AppConfig) error {
	if cfg.Version < 1 {
		// v0 → v1: set initial baseline defaults
		if cfg.Preferences.DebounceSec <= 0 {
			cfg.Preferences.DebounceSec = 30
		}
		if cfg.Preferences.LogMaxSizeMB <= 0 {
			cfg.Preferences.LogMaxSizeMB = 5
		}
		if cfg.Preferences.LogMaxFiles <= 0 {
			cfg.Preferences.LogMaxFiles = 3
		}
		if cfg.Preferences.Theme == "" {
			cfg.Preferences.Theme = "dark"
		}
		for i := range cfg.Repos {
			if cfg.Repos[i].Branch == "" {
				cfg.Repos[i].Branch = "main"
			}
			if cfg.Repos[i].Visibility == "" {
				cfg.Repos[i].Visibility = "private"
			}
			if cfg.Repos[i].CreatedAt.IsZero() {
				cfg.Repos[i].CreatedAt = time.Now()
			}
		}
		cfg.Version = 1
	}

	if cfg.Version < 2 {
		// v1 → v2: debounce trailing cap, large file safeguards, remote verification caching
		if cfg.Preferences.MaxDebounceSec <= 0 {
			cfg.Preferences.MaxDebounceSec = 120
		}
		if cfg.Preferences.LargeFileThresholdMB <= 0 {
			cfg.Preferences.LargeFileThresholdMB = 95
		}
		cfg.Preferences.ExcludeLargeFiles = true
		cfg.Preferences.ShowNotifications = true

		for i := range cfg.Accounts {
			if cfg.Accounts[i].CreatedAt.IsZero() {
				cfg.Accounts[i].CreatedAt = time.Now()
			}
		}
		cfg.Version = 2
	}

	if cfg.Version < 3 {
		// v2 → v3: Clean up orphan remote repos referencing accounts that were previously removed,
		// and deduplicate any duplicate remote entries.
		validAccounts := make(map[string]bool)
		for _, acc := range cfg.Accounts {
			validAccounts[acc.ID] = true
		}

		cleanRepos := make([]RepoConfig, 0, len(cfg.Repos))
		seenName := make(map[string]bool)

		for _, r := range cfg.Repos {
			// If it's a remote-only repo whose account no longer exists in GitSync, drop it
			if strings.TrimSpace(r.LocalPath) == "" && !validAccounts[r.AccountID] {
				continue
			}
			// Deduplicate remote-only repos with identical remote_name
			if strings.TrimSpace(r.LocalPath) == "" {
				key := strings.ToLower(r.RemoteName)
				if seenName[key] {
					continue
				}
				seenName[key] = true
			}
			cleanRepos = append(cleanRepos, r)
		}
		cfg.Repos = cleanRepos
		cfg.Version = 3
	}

	return nil
}
