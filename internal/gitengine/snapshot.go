package gitengine

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/gitsync/internal/config"
)

// SnapshotInfo describes a pre-sync safety backup snapshot.
type SnapshotInfo struct {
	Timestamp string    `json:"timestamp"`
	Type      string    `json:"type"` // "ref" or "zip"
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"created_at"`
}

// Snapshot creates both a lightweight git backup ref (refs/gitsync/backups/<timestamp>)
// and a zipped archive in the backup directory before synchronization.
func (e *Engine) Snapshot(repo *config.RepoConfig) error {
	cfg := e.snapshotConfig(repo)
	if !cfg.enabled {
		if e != nil && e.log != nil && repo != nil {
			e.log.Debug("gitengine", "[%s] pre-sync snapshot skipped (backups disabled in preferences)", repo.RemoteName)
		}
		return nil
	}

	// 1. Create lightweight git backup ref if git is available and repo has commits
	if e.exec != nil && e.exec.IsRepo(repo.LocalPath) {
		if err := e.createGitRefSnapshot(repo); err != nil {
			if e.log != nil {
				e.log.Debug("gitengine", "[%s] git ref snapshot skipped: %v", repo.RemoteName, err)
			}
		}
	}

	// 2. Create filesystem zip backup archive
	return e.createZipSnapshot(repo)
}

func (e *Engine) createGitRefSnapshot(repo *config.RepoConfig) error {
	dir := repo.LocalPath
	headCommit, err := e.exec.Run(dir, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(headCommit) == "" {
		return nil // No commits in repo yet
	}

	stamp := time.Now().Format("20060102-150405")
	refName := fmt.Sprintf("refs/gitsync/backups/%s", stamp)

	if _, err := e.exec.Run(dir, "update-ref", refName, strings.TrimSpace(headCommit)); err != nil {
		return fmt.Errorf("create ref %s: %w", refName, err)
	}

	if e.log != nil {
		commitShort := headCommit
		if len(commitShort) > 7 {
			commitShort = commitShort[:7]
		}
		e.log.Info("gitengine", "[%s] created backup ref %s -> %s", repo.RemoteName, refName, commitShort)
	}

	e.pruneGitRefSnapshots(dir, 10)
	return nil
}

func (e *Engine) pruneGitRefSnapshots(dir string, maxRefs int) {
	out, err := e.exec.Run(dir, "for-each-ref", "--format=%(refname)", "--sort=-creatordate", "refs/gitsync/backups")
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var refs []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			refs = append(refs, l)
		}
	}

	if len(refs) > maxRefs {
		for _, oldRef := range refs[maxRefs:] {
			_, _ = e.exec.Run(dir, "update-ref", "-d", oldRef)
		}
	}
}

func (e *Engine) createZipSnapshot(repo *config.RepoConfig) error {
	cfg := e.snapshotConfig(repo)
	if !cfg.enabled {
		return nil
	}

	if err := os.MkdirAll(cfg.backupDir, 0755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	stamp := time.Now().Format("20060102-150405")
	zipPath := filepath.Join(cfg.backupDir, fmt.Sprintf("%s-%s.zip", sanitizeName(repo.RemoteName), stamp))

	zf, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("create zip: %w", err)
	}
	defer zf.Close()

	w := zip.NewWriter(zf)
	defer w.Close()

	err = filepath.WalkDir(repo.LocalPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		base := d.Name()

		// Skip heavy or internal directories
		if d.IsDir() && (base == ".git" || base == "node_modules" || base == "vendor" ||
			base == "__pycache__" || base == ".venv" || base == "dist" || base == "build") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(repo.LocalPath, path)
		if err != nil {
			return nil
		}

		fw, err := w.Create(rel)
		if err != nil {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		_, _ = io.Copy(fw, f)
		return nil
	})
	if err != nil {
		return err
	}

	if e.log != nil {
		e.log.Info("gitengine", "snapshot: %s", zipPath)
	}
	if e.onSnapshot != nil {
		e.onSnapshot(repo.RemoteName, filepath.Base(zipPath))
	}
	e.pruneOldSnapshots(cfg.backupDir, sanitizeName(repo.RemoteName))
	return nil
}

type snapshotCfg struct {
	enabled   bool
	backupDir string
}

func (e *Engine) snapshotConfig(repo *config.RepoConfig) snapshotCfg {
	enabled := true
	backupDir := ""
	if repo != nil {
		backupDir = repo.BackupDir
	}

	if e != nil && e.cfg != nil {
		prefs := e.cfg.Config().Preferences
		enabled = prefs.BackupsEnabled
		if backupDir == "" && prefs.BackupDir != "" && repo != nil {
			backupDir = filepath.Join(prefs.BackupDir, sanitizeName(repo.RemoteName))
		}
	} else if cfgStore, err := config.NewStore(); err == nil {
		prefs := cfgStore.Config().Preferences
		enabled = prefs.BackupsEnabled
		if backupDir == "" && prefs.BackupDir != "" && repo != nil {
			backupDir = filepath.Join(prefs.BackupDir, sanitizeName(repo.RemoteName))
		}
	}

	if backupDir == "" && repo != nil {
		home, _ := os.UserHomeDir()
		backupDir = filepath.Join(home, ".gitsync", "backups", sanitizeName(repo.RemoteName))
	}
	return snapshotCfg{enabled: enabled, backupDir: backupDir}
}

// pruneOldSnapshots keeps only the last 5 snapshots for a given repo.
func (e *Engine) pruneOldSnapshots(backupDir, repoName string) {
	pattern := filepath.Join(backupDir, repoName+"-*.zip")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) <= 5 {
		return
	}
	// matches are sorted lexicographically (timestamps make this chronological)
	for _, old := range matches[:len(matches)-5] {
		os.Remove(old)
	}
}

func sanitizeName(name string) string {
	return strings.NewReplacer("/", "-", "\\", "-", " ", "_").Replace(name)
}
