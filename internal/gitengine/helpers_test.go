package gitengine

import (
	"os"
	"path/filepath"

	"github.com/user/gitsync/internal/config"
	"github.com/user/gitsync/internal/logger"
)

func init() {
	// Clean up any test artifacts from ~/.gitsync
	if home, err := os.UserHomeDir(); err == nil {
		testGitDir := filepath.Join(home, ".gitsync", "bin", "git")
		_ = os.RemoveAll(testGitDir)
	}

	if cfgStore, err := config.NewStore(); err == nil {
		prefs := cfgStore.Config().Preferences
		if prefs.GitPath != "" {
			// If git_path does not exist, reset it
			if _, err := os.Stat(prefs.GitPath); os.IsNotExist(err) {
				prefs.GitPath = ""
				_ = cfgStore.UpdatePreferences(prefs)
			}
		}
	}
}

// newNilLogger creates a no-op logger for tests.
func newNilLogger() *logger.Logger {
	return logger.New(logger.Prefs{LogMaxSizeMB: 1, LogMaxFiles: 1}, "", nil)
}
