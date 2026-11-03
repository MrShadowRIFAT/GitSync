package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/user/gitsync/internal/config"
	"github.com/user/gitsync/internal/gitengine"
	"github.com/user/gitsync/internal/githubapi"
	"github.com/user/gitsync/internal/logger"
)

// RepoStatus is the live state of a watched repo, emitted to the frontend.
type RepoStatus struct {
	RepoID       string    `json:"repo_id"`
	LocalPath    string    `json:"local_path"`
	RemoteName   string    `json:"remote_name"`
	State        string    `json:"state"` // "idle" | "debouncing" | "syncing" | "offline" | "error" | "conflict"
	LastSyncAt   time.Time `json:"last_sync_at"`
	LastError    string    `json:"last_error"`
	PendingFiles int       `json:"pending_files"`
}

// repoSyncState holds per-repo concurrency locks and dirty/resync flags.
type repoSyncState struct {
	mu          sync.Mutex // guarantees only one sync runs on this repo at a time
	needsResync bool       // dirty flag: set when changes or triggers arrive during an active sync
	status      RepoStatus
}

// Service manages the recursive directory watcher and orchestrates debounced syncs.
type Service struct {
	cfg         *config.Store
	git         *gitengine.Engine
	github      *githubapi.ClientPool
	log         *logger.Logger
	emit        func(string, interface{})
	watcher     *fsnotify.Watcher
	debouncer   *Debouncer
	states      map[string]*repoSyncState
	pathToRepo  map[string]string // watched directory path -> repo ID
	mu          sync.RWMutex
	activeSyncs sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

// New creates a new watcher Service with trailing debounce cap.
func New(
	cfg *config.Store,
	git *gitengine.Engine,
	gh *githubapi.ClientPool,
	log *logger.Logger,
	emit func(string, interface{}),
) *Service {
	prefs := cfg.Config().Preferences
	debounceSec := prefs.DebounceSec
	if debounceSec <= 0 {
		debounceSec = 30
	}
	maxDebounceSec := prefs.MaxDebounceSec
	if maxDebounceSec <= 0 || maxDebounceSec < debounceSec {
		maxDebounceSec = 120
	}

	if git != nil {
		git.SetConfigStore(cfg)
		git.SetOnSnapshot(func(repoName, target string) {
			if emit != nil {
				emit("toast:notify", map[string]interface{}{
					"id":        fmt.Sprintf("backup-%s-%d", repoName, time.Now().UnixNano()),
					"type":      "backup",
					"title":     "Backup Snapshot Created",
					"message":   fmt.Sprintf("[%s] Safety backup snapshot archived (%s)", repoName, target),
					"repoName":  repoName,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
			}
		})
	}

	return &Service{
		cfg:        cfg,
		git:        git,
		github:     gh,
		log:        log,
		emit:       emit,
		states:     make(map[string]*repoSyncState),
		pathToRepo: make(map[string]string),
		debouncer: NewDebouncer(
			time.Duration(debounceSec)*time.Second,
			time.Duration(maxDebounceSec)*time.Second,
		),
	}
}

// Start initializes the fsnotify watcher and starts the event loop.
func (s *Service) Start(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)

	var err error
	s.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		s.log.Error("watcher", "failed to create fsnotify watcher: %v", err)
		return
	}

	cfg := s.cfg.Config()
	for _, repo := range cfg.Repos {
		repo := repo
		if strings.TrimSpace(repo.LocalPath) == "" {
			s.registerRemoteOnlyRepo(&repo)
			continue
		}
		if !repo.Enabled {
			s.registerPausedRepo(&repo)
			continue
		}
		s.registerRepo(&repo)
	}

	go s.eventLoop(s.ctx)
	s.log.Info("watcher", "started, watching %d repos", len(cfg.Repos))
}

// Stop halts the watcher and drains active syncs up to timeout.
func (s *Service) Stop(ctx context.Context) {
	if s.cancel != nil {
		s.cancel()
	}
	s.debouncer.CancelAll()
	if s.watcher != nil {
		_ = s.watcher.Close()
	}

	// Drain active sync goroutines with graceful timeout
	doneCh := make(chan struct{})
	go func() {
		s.activeSyncs.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		s.log.Info("watcher", "all active syncs completed cleanly")
	case <-ctx.Done():
		s.log.Warn("watcher", "stop context timed out waiting for active syncs")
	}
	s.log.Info("watcher", "stopped")
}

// TriggerSync manually triggers an immediate sync for a repo.
func (s *Service) TriggerSync(repoID string) error {
	repo := s.cfg.FindRepo(repoID)
	if repo == nil {
		return os.ErrNotExist
	}
	if strings.TrimSpace(repo.LocalPath) == "" {
		return nil
	}
	// Cancel any pending debounce timer since manual sync takes precedence
	s.debouncer.Cancel(repoID)

	s.activeSyncs.Add(1)
	go func() {
		defer s.activeSyncs.Done()
		s.runSync(repoID)
	}()
	return nil
}

// Statuses returns a snapshot of all repo statuses.
func (s *Service) Statuses() []RepoStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RepoStatus, 0, len(s.states))
	for _, st := range s.states {
		out = append(out, st.status)
	}
	return out
}

// AddRepo registers a new repo for watching at runtime and triggers an initial sync.
func (s *Service) AddRepo(repo *config.RepoConfig) {
	if repo == nil {
		return
	}
	if strings.TrimSpace(repo.LocalPath) == "" {
		s.registerRemoteOnlyRepo(repo)
		s.emitStatuses()
		return
	}
	s.registerRepo(repo)
	s.emitStatuses()
	_ = s.TriggerSync(repo.ID)
}

// WatchClonedRepo starts watching a newly cloned repository without triggering an immediate redundant push.
func (s *Service) WatchClonedRepo(repo *config.RepoConfig) {
	if repo == nil || strings.TrimSpace(repo.LocalPath) == "" {
		return
	}
	s.registerRepo(repo)
	s.mu.Lock()
	if st, ok := s.states[repo.ID]; ok {
		st.status.State = "idle"
		st.status.LastSyncAt = time.Now()
		st.status.LastError = ""
	}
	s.mu.Unlock()
	s.emitStatuses()
}

// TriggerPull manually fetches and rebases the latest remote changes for a repository.
func (s *Service) TriggerPull(repoID string) error {
	repo := s.cfg.FindRepo(repoID)
	if repo == nil {
		return os.ErrNotExist
	}
	if strings.TrimSpace(repo.LocalPath) == "" {
		return fmt.Errorf("repository is not downloaded locally yet")
	}

	s.debouncer.Cancel(repoID)
	s.activeSyncs.Add(1)
	go func() {
		defer s.activeSyncs.Done()
		s.mu.RLock()
		state, exists := s.states[repoID]
		s.mu.RUnlock()
		if !exists {
			return
		}
		if !state.mu.TryLock() {
			return
		}
		defer state.mu.Unlock()

		s.mu.Lock()
		state.status.State = "syncing"
		s.mu.Unlock()
		s.emitStatuses()

		account := s.cfg.FindAccount(repo.AccountID)
		pullCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := s.git.Pull(pullCtx, repo, account, s.github)
		cancel()

		s.mu.Lock()
		if err != nil {
			state.status.State = "error"
			state.status.LastError = err.Error()
			if s.log != nil {
				s.log.Error("sync", "[%s] pull failed: %v", repo.RemoteName, err)
			}
		} else {
			state.status.State = "idle"
			state.status.LastSyncAt = time.Now()
			state.status.LastError = ""
			if s.emit != nil {
				s.emit("toast:notify", map[string]interface{}{
					"id":        fmt.Sprintf("pull-%s-%d", repo.RemoteName, time.Now().UnixNano()),
					"type":      "success",
					"title":     "Pulled Latest Changes",
					"message":   fmt.Sprintf("[%s] Pulled latest changes from GitHub", repo.RemoteName),
					"repoName":  repo.RemoteName,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
			}
		}
		s.mu.Unlock()
		s.emitStatuses()
	}()
	return nil
}

// PauseRepo stops watching a repository's filesystem events while keeping its status visible as "paused".
func (s *Service) PauseRepo(repoID string) {
	s.debouncer.Cancel(repoID)

	s.mu.Lock()
	if st, ok := s.states[repoID]; ok {
		st.status.State = "paused"
		st.status.PendingFiles = 0
	}
	for path, id := range s.pathToRepo {
		if id == repoID {
			if s.watcher != nil {
				_ = s.watcher.Remove(path)
			}
			delete(s.pathToRepo, path)
		}
	}
	s.mu.Unlock()
	s.emitStatuses()
}

// SetGitEngine updates the active git engine instance used by the watcher.
func (s *Service) SetGitEngine(git *gitengine.Engine) {
	if s == nil {
		return
	}
	if git != nil {
		git.SetConfigStore(s.cfg)
		git.SetOnSnapshot(func(repoName, target string) {
			if s.emit != nil {
				s.emit("toast:notify", map[string]interface{}{
					"id":        fmt.Sprintf("backup-%s-%d", repoName, time.Now().UnixNano()),
					"type":      "backup",
					"title":     "Backup Snapshot Created",
					"message":   fmt.Sprintf("[%s] Safety backup snapshot archived (%s)", repoName, target),
					"repoName":  repoName,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
			}
		})
	}
	s.mu.Lock()
	s.git = git
	s.mu.Unlock()
}

// UpdateDebounceIntervals dynamically updates the running debouncer's timing intervals.
func (s *Service) UpdateDebounceIntervals(debounceSec, maxDebounceSec int) {
	if s == nil || s.debouncer == nil {
		return
	}
	if debounceSec <= 0 {
		debounceSec = 30
	}
	if maxDebounceSec <= 0 || maxDebounceSec < debounceSec {
		maxDebounceSec = debounceSec * 4
		if maxDebounceSec < 120 {
			maxDebounceSec = 120
		}
	}
	s.debouncer.SetIntervals(
		time.Duration(debounceSec)*time.Second,
		time.Duration(maxDebounceSec)*time.Second,
	)
	if s.log != nil {
		s.log.Info("watcher", "updated debounce intervals: idle=%ds, maxWait=%ds", debounceSec, maxDebounceSec)
	}
}

// RemoveRepo unregisters a repo from watching.
func (s *Service) RemoveRepo(repoID string) {
	s.debouncer.Cancel(repoID)

	s.mu.Lock()
	delete(s.states, repoID)
	for path, id := range s.pathToRepo {
		if id == repoID {
			if s.watcher != nil {
				_ = s.watcher.Remove(path)
			}
			delete(s.pathToRepo, path)
		}
	}
	s.mu.Unlock()
	s.emitStatuses()
}

// ─── Internal Implementation ──────────────────────────────────────────────────

func (s *Service) registerRemoteOnlyRepo(repo *config.RepoConfig) {
	s.mu.Lock()
	s.states[repo.ID] = &repoSyncState{
		status: RepoStatus{
			RepoID:     repo.ID,
			LocalPath:  "",
			RemoteName: repo.RemoteName,
			State:      "idle",
			LastSyncAt: repo.CreatedAt,
		},
	}
	s.mu.Unlock()
}

func (s *Service) registerPausedRepo(repo *config.RepoConfig) {
	s.mu.Lock()
	s.states[repo.ID] = &repoSyncState{
		status: RepoStatus{
			RepoID:     repo.ID,
			LocalPath:  repo.LocalPath,
			RemoteName: repo.RemoteName,
			State:      "paused",
		},
	}
	s.mu.Unlock()
}

func (s *Service) registerRepo(repo *config.RepoConfig) {
	// 1. Automatic git init if not already a repository
	s.git.EnsureInit(repo)
	ensureDefaultGitignore(repo.LocalPath)

	// 2. Register recursive directory paths with fsnotify
	if err := s.registerPath(repo.ID, repo.LocalPath); err != nil {
		s.log.Error("watcher", "failed to watch %s: %v", repo.LocalPath, err)
		return
	}

	// 3. Initialize or update state machine for this repo
	s.mu.Lock()
	if st, exists := s.states[repo.ID]; exists {
		st.status.LocalPath = repo.LocalPath
		st.status.RemoteName = repo.RemoteName
		st.status.State = "idle"
	} else {
		s.states[repo.ID] = &repoSyncState{
			status: RepoStatus{
				RepoID:     repo.ID,
				LocalPath:  repo.LocalPath,
				RemoteName: repo.RemoteName,
				State:      "idle",
			},
		}
	}
	s.mu.Unlock()

	s.log.Info("watcher", "watching: %s → %s", repo.LocalPath, repo.RemoteName)
}

func (s *Service) registerPath(repoID, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if isGitPath(path) || isIgnoredDir(d.Name()) {
			return filepath.SkipDir
		}
		if s.watcher != nil {
			if watchErr := s.watcher.Add(path); watchErr != nil {
				return nil
			}
		}
		s.mu.Lock()
		s.pathToRepo[path] = repoID
		s.mu.Unlock()
		return nil
	})
}

func (s *Service) eventLoop(ctx context.Context) {
	defer func() {
		if s.watcher != nil {
			_ = s.watcher.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			s.debouncer.CancelAll()
			return

		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			s.handleEvent(event)

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			s.log.Error("watcher", "fsnotify error: %v", err)
		}
	}
}

func (s *Service) handleEvent(event fsnotify.Event) {
	// Ignore permission / metadata-only changes
	if event.Has(fsnotify.Chmod) {
		return
	}

	// 1. Strict .git sanitization: reject any path containing .git
	if isGitPath(event.Name) {
		return
	}

	// 2. Reject noise file names and extensions
	base := filepath.Base(event.Name)
	if isNoisePath(base) {
		return
	}

	// 3. Dynamically watch and auto-init newly created subdirectories
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if !isIgnoredDir(base) {
				repoID := s.findRepoForPath(filepath.Dir(event.Name))
				if repoID != "" {
					_ = s.registerPath(repoID, event.Name)
					if repo := s.cfg.FindRepo(repoID); repo != nil {
						// Auto git init on newly discovered workspace subfolders if not already part of a repo
						_ = s.git.EnsureInitPath(event.Name, repo)
					}
				}
			}
			return
		}
	}

	// 4. Check global pause
	if s.cfg.Config().Preferences.GlobalPause {
		return
	}

	// 5. Resolve repo ID for the changed file path
	repoID := s.findRepoForPath(filepath.Dir(event.Name))
	if repoID == "" {
		repoID = s.findRepoForPath(event.Name)
	}
	if repoID == "" {
		return
	}

	// 6. Check per-repo enabled state
	repo := s.cfg.FindRepo(repoID)
	if repo == nil || !repo.Enabled {
		return
	}

	// 7. Update pending file count & debouncing state
	s.mu.Lock()
	if st, ok := s.states[repoID]; ok {
		st.status.PendingFiles++
		if st.status.State == "idle" {
			st.status.State = "debouncing"
		}
	}
	s.mu.Unlock()
	s.emitStatuses()

	// 8. Schedule debounced execution with trailing cap protection
	s.debouncer.Schedule(repoID, func() {
		s.activeSyncs.Add(1)
		go func() {
			defer s.activeSyncs.Done()
			s.runSync(repoID)
		}()
	})
}

// runSync executes the sync workflow with mutual exclusion and the dirty/needsResync flag.
func (s *Service) runSync(repoID string) {
	s.mu.RLock()
	state, exists := s.states[repoID]
	s.mu.RUnlock()
	if !exists {
		return
	}

	// Attempt to acquire per-repo execution lock
	if !state.mu.TryLock() {
		// Another sync is currently in progress: flag dirty so it immediately re-syncs when finished!
		state.needsResync = true
		s.log.Debug("watcher", "[%s] sync currently active; queued follow-up sync", repoID)
		return
	}
	defer state.mu.Unlock()

	// Loop as long as changes arrive while syncing (needsResync)
	for {
		state.needsResync = false

		s.mu.Lock()
		state.status.State = "syncing"
		state.status.PendingFiles = 0
		s.mu.Unlock()
		s.emitStatuses()

		repo := s.cfg.FindRepo(repoID)
		if repo == nil {
			return
		}
		account := s.cfg.FindAccount(repo.AccountID)

		// File settling probe: brief pause to allow external file write handles to flush
		time.Sleep(50 * time.Millisecond)

		syncCtx, syncCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := s.git.Sync(syncCtx, repo, account, s.github)
		syncCancel()

		s.mu.Lock()
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "offline") || strings.Contains(errStr, "Could not resolve host") {
				state.status.State = "offline"
			} else if strings.Contains(errStr, "conflict") {
				state.status.State = "conflict"
				if s.emit != nil {
					s.emit("toast:notify", map[string]interface{}{
						"id":        fmt.Sprintf("conflict-%s-%d", repo.RemoteName, time.Now().UnixNano()),
						"type":      "conflict",
						"title":     "Git Conflict Isolated",
						"message":   fmt.Sprintf("[%s] Changes safely preserved in conflict branch", repo.RemoteName),
						"repoName":  repo.RemoteName,
						"timestamp": time.Now().UTC().Format(time.RFC3339),
					})
				}
			} else {
				state.status.State = "error"
			}
			state.status.LastError = errStr
			s.log.Error("sync", "[%s] sync failed: %v", repo.RemoteName, err)
		} else {
			state.status.State = "idle"
			state.status.LastSyncAt = time.Now()
			state.status.LastError = ""
			s.log.Info("sync", "[%s] synced successfully", repo.RemoteName)
			if s.emit != nil {
				s.emit("toast:notify", map[string]interface{}{
					"id":        fmt.Sprintf("sync-%s-%d", repo.RemoteName, time.Now().UnixNano()),
					"type":      "success",
					"title":     "Repository Synced",
					"message":   fmt.Sprintf("[%s] Synced successfully to GitHub", repo.RemoteName),
					"repoName":  repo.RemoteName,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
			}
		}
		s.mu.Unlock()
		s.emitStatuses()

		// If no new events arrived during sync, we are finished!
		if !state.needsResync {
			break
		}
		s.log.Info("sync", "[%s] dirty flag set during sync, executing trailing pass", repo.RemoteName)
	}
}

func (s *Service) findRepoForPath(path string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clean := filepath.Clean(path)
	// Check exact match
	if id, ok := s.pathToRepo[clean]; ok {
		return id
	}

	// Check ancestors
	for p := clean; p != filepath.Dir(p); p = filepath.Dir(p) {
		if id, ok := s.pathToRepo[p]; ok {
			return id
		}
	}
	return ""
}

func (s *Service) emitStatuses() {
	if s.emit != nil {
		s.emit("repo:status", s.Statuses())
	}
}

// ─── Path & Noise Filters ─────────────────────────────────────────────────────

// isGitPath returns true if the path is inside or is a .git directory.
func isGitPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasSuffix(clean, "/.git") ||
		strings.Contains(clean, "/.git/") ||
		filepath.Base(clean) == ".git"
}

// isIgnoredDir returns true for heavy build or dependency directories.
func isIgnoredDir(base string) bool {
	switch base {
	case ".git", "node_modules", "vendor", "__pycache__", ".venv",
		".idea", ".vscode", "dist", "build", "target", ".next", ".nuxt", "bin", "obj":
		return true
	}
	return strings.HasPrefix(base, ".")
}

// isNoisePath returns true for temporary, editor, or system noise files.
func isNoisePath(base string) bool {
	noiseSuffixes := []string{
		".tmp", ".swp", ".lock", "~", ".bak", ".crdownload",
		".part", ".ds_store", "thumbs.db", "desktop.ini",
	}
	noisePrefixes := []string{".", "#", "~$"}

	lower := strings.ToLower(base)
	for _, sfx := range noiseSuffixes {
		if strings.HasSuffix(lower, sfx) {
			return true
		}
	}
	for _, pfx := range noisePrefixes {
		if strings.HasPrefix(base, pfx) {
			return true
		}
	}
	return false
}

// ensureDefaultGitignore creates a curated .gitignore if one does not exist.
func ensureDefaultGitignore(repoPath string) {
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		return
	}

	defaultContent := `# GitSync default exclusions
node_modules/
vendor/
__pycache__/
*.pyc
.venv/
dist/
build/
.DS_Store
Thumbs.db
*.tmp
*.swp
*.log
.env
.env.local
`
	_ = os.WriteFile(gitignorePath, []byte(defaultContent), 0644)
}
