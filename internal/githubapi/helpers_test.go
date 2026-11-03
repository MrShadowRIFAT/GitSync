package githubapi

import "github.com/user/gitsync/internal/logger"

func newNilLogger() *logger.Logger {
	return logger.New(logger.Prefs{LogMaxSizeMB: 1, LogMaxFiles: 1}, "", nil)
}
