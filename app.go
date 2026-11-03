package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	stdRuntime "runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/options"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/user/gitsync/internal/aireadme"
	"github.com/user/gitsync/internal/autostart"
	"github.com/user/gitsync/internal/config"
	"github.com/user/gitsync/internal/githubapi"
	"github.com/user/gitsync/internal/gitengine"
	"github.com/user/gitsync/internal/logger"
	"github.com/user/gitsync/internal/tray"
	"github.com/user/gitsync/internal/updater"
	"github.com/user/gitsync/internal/watcher"
)

// App is the central application controller bound to Wails.
type App struct {
	ctx        context.Context
	cancel     context.CancelFunc
	cfg        *config.Store
	log        *logger.Logger
	git        *gitengine.Engine
	github     *githubapi.ClientPool
	watchSvc   *watcher.Service
	updater    *updater.Service
	isQuitting bool
	quitMu     sync.Mutex
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// Startup is called by Wails when the runtime is ready.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	_, a.cancel = context.WithCancel(ctx)

	// 1. Initialize configuration store
	var cfgErr error
	a.cfg, cfgErr = config.NewStore()
	if cfgErr != nil {
		panic(fmt.Sprintf("failed to initialize config store: %v", cfgErr))
	}

	// 2. Initialize file and event logger
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".gitsync", "logs")
	prefs := a.cfg.Config().Preferences
	a.log = logger.New(logger.Prefs{
		LogMaxSizeMB: prefs.LogMaxSizeMB,
		LogMaxFiles:  prefs.LogMaxFiles,
	}, logDir, a.emitEvent)

	a.log.Info("app", "GitSync starting up")

	// 3. Clean up stale .git/index.lock files from previous crashes or ungraceful kills
	a.cleanupStaleIndexLocks()

	// 4. Initialize Git engine with portable/custom Git support
	a.git = gitengine.New(a.log, prefs.GitPath)
	a.git.SetConfigStore(a.cfg)
	if !a.git.Available() {
		a.log.Warn("app", "Git binary not found — please ensure Git is installed or configure a custom path")
	}

	// 5. Initialize GitHub multi-account client pool & custom protocol handler
	_ = autostart.RegisterProtocolHandler()
	a.github = githubapi.NewPool(a.cfg, a.log)
	if a.github.OAuth() != nil {
		a.github.OAuth().SetOnFocus(a.focusWindow)
	}

	// 6. Initialize and start the background watcher service
	a.watchSvc = watcher.New(a.cfg, a.git, a.github, a.log, a.emitEvent)
	a.watchSvc.Start(a.ctx)

	// 7. Start system tray integration in a background goroutine
	go tray.Run(a.ctx, a.cfg, a.log, a.showWindow, a.hideWindow, a.quitApp, a.emitEvent, a.syncAllRepos)

	// 8. Start periodic tray metrics sync ticker
	go a.startTrayMetricsLoop()

	// 9. Start background 24-hour auto-updater
	a.updater = updater.New(a.log, a.emitEvent)
	a.updater.Start(a.ctx)

	a.log.Info("app", "GitSync initialized successfully")
}

// startup is an alias to Startup for lifecycle hooks.
func (a *App) startup(ctx context.Context) {
	a.Startup(ctx)
}

// domReady is called after the frontend DOM is fully loaded.
func (a *App) domReady(ctx context.Context) {
	if a.cfg.Config().Preferences.StartMinimized || hasMinimizedFlag() {
		wailsRuntime.Hide(ctx)
	}

	// Listen for native file/folder drops and emit resolved directory path
	wailsRuntime.OnFileDrop(ctx, func(x, y int, paths []string) {
		if len(paths) == 0 {
			return
		}
		raw := strings.TrimSpace(paths[0])
		if raw == "" {
			return
		}
		cleaned := filepath.Clean(raw)
		if info, err := os.Stat(cleaned); err == nil && !info.IsDir() {
			cleaned = filepath.Dir(cleaned)
		}
		a.log.Info("app", "folder dropped: "+cleaned)
		a.emitEvent("folder:dropped", cleaned)
	})

	// Push initial state snapshot to frontend
	a.emitEvent("repo:status", a.watchSvc.Statuses())
	a.emitEvent("app:ready", map[string]interface{}{
		"gitAvailable": a.git.Available(),
		"version":      "1.0.0",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	})

	// Discover remote GitHub repositories for all connected accounts (without pulling/downloading)
	go a.syncAllAccountsRemoteRepos()
}

func hasMinimizedFlag() bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(strings.TrimSpace(arg), "--minimized") {
			return true
		}
	}
	return false
}

// beforeClose intercepts the window close event.
// Unless QuitApp() was explicitly invoked, we hide the window to tray instead.
func (a *App) beforeClose(ctx context.Context) bool {
	a.quitMu.Lock()
	defer a.quitMu.Unlock()

	if a.isQuitting {
		return false // Allow destruction and shutdown
	}
	wailsRuntime.Hide(ctx)
	return true // Cancel close event (keep running in tray)
}

// shutdown is called during application termination.
func (a *App) shutdown(ctx context.Context) {
	a.log.Info("app", "GitSync shutting down...")
	if a.cancel != nil {
		a.cancel()
	}

	// Allow in-flight git sync operations up to 8 seconds to cleanly drain
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if a.watchSvc != nil {
		a.watchSvc.Stop(shutdownCtx)
	}

	if a.log != nil {
		a.log.Flush()
		a.log.Close()
	}
	if a.cfg != nil {
		_ = a.cfg.Save()
	}
}

// ─── Window Controls (for frameless titlebar) ──────────────────────────────────

// WindowMinimise minimizes the Wails window.
func (a *App) WindowMinimise() {
	if a.ctx != nil {
		wailsRuntime.WindowMinimise(a.ctx)
	}
}

// WindowToggleMaximise toggles between maximized and restored window states.
func (a *App) WindowToggleMaximise() {
	if a.ctx != nil {
		wailsRuntime.WindowToggleMaximise(a.ctx)
	}
}

// WindowClose hides the window to the system tray.
func (a *App) WindowClose() {
	a.hideWindow()
}

// QuitApp signals an intentional exit and closes the application completely.
func (a *App) QuitApp() {
	a.quitMu.Lock()
	a.isQuitting = true
	a.quitMu.Unlock()

	if a.ctx != nil {
		wailsRuntime.Quit(a.ctx)
	}
}

func (a *App) showWindow() {
	if a.ctx != nil {
		wailsRuntime.Show(a.ctx)
	}
}

func (a *App) focusWindow() {
	if a.ctx != nil {
		wailsRuntime.WindowUnminimise(a.ctx)
		wailsRuntime.Show(a.ctx)
		wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)
		go func() {
			time.Sleep(250 * time.Millisecond)
			if a.ctx != nil {
				wailsRuntime.WindowSetAlwaysOnTop(a.ctx, false)
			}
		}()
	}
}

func (a *App) onSecondInstanceLaunch(_ options.SecondInstanceData) {
	a.focusWindow()
}

func (a *App) hideWindow() {
	if a.ctx != nil {
		wailsRuntime.Hide(a.ctx)
	}
}

func (a *App) quitApp() {
	a.QuitApp()
}

func (a *App) syncAllRepos() {
	_ = a.SyncAllNow()
}

// ─── Custom Event Bus ─────────────────────────────────────────────────────────

// emitEvent dispatches a typed event and payload to the Wails frontend.
func (a *App) emitEvent(name string, data interface{}) {
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, name, data)
	}
	if name == "repo:status" || name == "global_pause:changed" || name == "config:updated" {
		tray.UpdateMetrics(a.calculateTrayMetrics())
	}
}

func (a *App) calculateTrayMetrics() tray.Metrics {
	if a.cfg == nil || a.watchSvc == nil {
		return tray.Metrics{}
	}
	cfg := a.cfg.Config()
	statuses := a.watchSvc.Statuses()

	m := tray.Metrics{
		TotalRepos: len(cfg.Repos),
		IsPaused:   cfg.Preferences.GlobalPause,
	}

	for _, r := range cfg.Repos {
		if r.Enabled {
			m.ActiveRepos++
		}
	}

	for _, st := range statuses {
		if st.State == "syncing" {
			m.Syncing++
		}
		if st.State == "conflict" {
			m.Conflicts++
		}
		if st.PendingFiles > 0 || st.State == "debouncing" {
			m.PendingSyncs += st.PendingFiles
			if st.PendingFiles == 0 {
				m.PendingSyncs++
			}
		}
	}

	return m
}

func (a *App) startTrayMetricsLoop() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			tray.UpdateMetrics(a.calculateTrayMetrics())
		}
	}
}

// ─── Exported Wails Backend Methods ───────────────────────────────────────────

// GetConfig returns the active configuration.
func (a *App) GetConfig() *config.AppConfig {
	return a.cfg.Config()
}

// GetRepoStatuses returns live synchronization statuses for all watched repositories.
func (a *App) GetRepoStatuses() []watcher.RepoStatus {
	return a.watchSvc.Statuses()
}

// GetLogs returns the most recent log entries from the in-memory ring buffer.
func (a *App) GetLogs(limit int) []logger.Entry {
	return a.log.Recent(limit)
}

// ClearLogs empties the in-memory log ring buffer.
func (a *App) ClearLogs() {
	if a.log != nil {
		a.log.Clear()
	}
}

// SyncNow triggers an immediate manual synchronization for a specific repository.
func (a *App) SyncNow(repoID string) error {
	return a.watchSvc.TriggerSync(repoID)
}

// SyncAllNow triggers an immediate manual synchronization across all enabled repositories.
func (a *App) SyncAllNow() error {
	if a.cfg == nil || a.watchSvc == nil {
		return nil
	}
	var firstErr error
	for _, r := range a.cfg.Config().Repos {
		if r.Enabled {
			if err := a.watchSvc.TriggerSync(r.ID); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// SetGlobalPause toggles the global pause state across all watched repositories.
func (a *App) SetGlobalPause(paused bool) error {
	prefs := a.cfg.Config().Preferences
	prefs.GlobalPause = paused
	if err := a.cfg.UpdatePreferences(prefs); err != nil {
		return err
	}
	tray.UpdatePauseState(paused)
	a.emitEvent("global_pause:changed", paused)
	a.log.Info("app", "global pause set to %v", paused)
	return nil
}

// ValidatePAT verifies a GitHub Personal Access Token and returns the authenticated login.
func (a *App) ValidatePAT(token string) (string, error) {
	return a.github.ValidatePAT(token)
}

// ValidateAndSaveGitHubAccount verifies the PAT against GitHub's /user API,
// extracts username and avatar, encrypts it via the AES-256-GCM vault, and saves it to config.
func (a *App) ValidateAndSaveGitHubAccount(tokenName string, patToken string) (config.Account, error) {
	patToken = strings.TrimSpace(patToken)
	if patToken == "" {
		return config.Account{}, fmt.Errorf("personal access token cannot be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	userInfo, rateInfo, err := a.github.VerifyToken(ctx, patToken)
	if err != nil {
		if a.log != nil {
			a.log.Warn("auth", "GitHub PAT validation failed: %v", err)
		}
		return config.Account{}, fmt.Errorf("GitHub authentication failed: %w", err)
	}

	return a.saveValidatedAccount(tokenName, patToken, userInfo, rateInfo)
}

// SavePATAccount validates a GitHub Personal Access Token against the /user API,
// encrypts it with the AES-256-GCM vault, and saves the account to persistent config.
func (a *App) SavePATAccount(name string, token string) (config.Account, error) {
	return a.ValidateAndSaveGitHubAccount(name, token)
}

// StartGitHubOAuth starts the local loopback OAuth server on port 8080 (http://127.0.0.1:8080/auth/callback),
// opens the user's default browser to GitHub's authorization consent page, exchanges the code with the OAuth App Client Secret,
// StartGitHubOAuth initiates the browser-based OAuth flow, waits for the callback token, validates it,
// and saves the account.
func (a *App) StartGitHubOAuth() (config.Account, error) {
	return a.StartGitHubOAuthWithHint("")
}

// StartGitHubOAuthWithHint initiates the browser-based OAuth flow with an optional GitHub username hint
// and multi-account switcher support when one or more accounts are already connected.
func (a *App) StartGitHubOAuthWithHint(loginHint string) (config.Account, error) {
	if a.github == nil || a.github.OAuth() == nil {
		return config.Account{}, fmt.Errorf("OAuth service not initialized")
	}

	addingAdditional := false
	if a.cfg != nil && len(a.cfg.Config().Accounts) > 0 {
		addingAdditional = true
	}

	oauth := a.github.OAuth()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	authURL, resChan, err := oauth.StartOAuthServerWithOptions(ctx, strings.TrimSpace(loginHint), addingAdditional)
	if err != nil {
		if a.log != nil {
			a.log.Warn("oauth", "Failed to start OAuth server: %v", err)
		}
		return config.Account{}, fmt.Errorf("failed to start OAuth listener: %w", err)
	}

	if a.log != nil {
		a.log.Info("oauth", "Started OAuth flow, launching system browser to %s", authURL)
	}

	// Launch default system browser
	if a.ctx != nil {
		wailsRuntime.BrowserOpenURL(a.ctx, authURL)
	}

	// Await callback or timeout
	select {
	case res := <-resChan:
		if res.Error != nil {
			if a.log != nil {
				a.log.Warn("oauth", "OAuth flow error: %v", res.Error)
			}
			return config.Account{}, res.Error
		}

		// Verify obtained token & fetch user profile
		verifyCtx, vCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer vCancel()

		userInfo, rateInfo, err := a.github.VerifyToken(verifyCtx, res.Token)
		if err != nil {
			return config.Account{}, fmt.Errorf("verify OAuth token: %w", err)
		}

		label := ""
		if userInfo.Name != "" {
			label = userInfo.Name
		} else {
			label = fmt.Sprintf("@%s", userInfo.Username)
		}

		account, err := a.saveValidatedAccount(label, res.Token, userInfo, rateInfo)
		if err != nil {
			return config.Account{}, err
		}

		if a.log != nil {
			a.log.Info("oauth", "Successfully linked GitHub account via OAuth: @%s (%s)", account.Username, account.Label)
		}
		return account, nil

	case <-ctx.Done():
		oauth.Cancel()
		return config.Account{}, fmt.Errorf("OAuth login timed out after 3 minutes")
	}
}

// CancelGitHubOAuth aborts any in-flight GitHub OAuth flow and shuts down the local loopback listener.
func (a *App) CancelGitHubOAuth() {
	if a.github != nil && a.github.OAuth() != nil {
		a.github.OAuth().Cancel()
		if a.log != nil {
			a.log.Info("oauth", "Cancelled active OAuth login request")
		}
	}
}

// saveValidatedAccount handles AES-256-GCM vault encryption, config persistence, and event emission.
func (a *App) saveValidatedAccount(tokenName string, patToken string, userInfo *githubapi.GitHubUserInfo, rateInfo *githubapi.RateLimitInfo) (config.Account, error) {
	encPAT, err := a.cfg.EncryptPAT(patToken)
	if err != nil {
		return config.Account{}, fmt.Errorf("encrypt PAT: %w", err)
	}

	label := strings.TrimSpace(tokenName)
	if label == "" {
		if userInfo.Name != "" {
			label = userInfo.Name
		} else {
			label = fmt.Sprintf("@%s", userInfo.Username)
		}
	}

	email := strings.TrimSpace(userInfo.Email)
	if email == "" {
		email = fmt.Sprintf("%s@users.noreply.github.com", userInfo.Username)
	}

	// If account with this username already exists, update token, avatar and email
	existing := a.cfg.FindAccountByUsername(userInfo.Username)
	var account config.Account
	if existing != nil {
		account = *existing
		account.Label = label
		account.EncryptedPAT = encPAT
		account.AvatarURL = userInfo.AvatarURL
		account.Email = email
		if err := a.cfg.UpdateAccount(account); err != nil {
			return config.Account{}, fmt.Errorf("update account: %w", err)
		}
		a.github.InvalidateClient(account.ID)
		if a.log != nil {
			a.log.Info("auth", "updated existing GitHub account: @%s (%s)", account.Username, account.Label)
		}
	} else {
		account = config.Account{
			ID:           uuid.New().String(),
			Label:        label,
			Username:     userInfo.Username,
			Email:        email,
			EncryptedPAT: encPAT,
			AvatarURL:    userInfo.AvatarURL,
			CreatedAt:    time.Now(),
		}
		if err := a.cfg.AddAccount(account); err != nil {
			return config.Account{}, fmt.Errorf("save account: %w", err)
		}
		if a.log != nil {
			a.log.Info("auth", "registered new GitHub account: @%s (%s)", account.Username, account.Label)
		}
	}

	a.emitEvent("config:updated", a.cfg.Config())
	if rateInfo != nil {
		a.emitEvent("account:ratelimit", map[string]interface{}{
			"accountId": account.ID,
			"remaining": rateInfo.Remaining,
			"limit":     rateInfo.Limit,
			"resetAt":   rateInfo.ResetAt.Format(time.RFC3339),
			"used":      rateInfo.Used,
		})
	}

	// Import all remote repositories owned by this account into the app list (without pulling/downloading)
	go a.importAccountRemoteRepos(account)

	return account, nil
}

// syncAllAccountsRemoteRepos imports remote repositories for all configured GitHub accounts.
func (a *App) syncAllAccountsRemoteRepos() {
	if a.cfg == nil || a.github == nil {
		return
	}
	accounts := a.cfg.Config().Accounts
	for _, acc := range accounts {
		a.importAccountRemoteRepos(acc)
	}
}

// importAccountRemoteRepos fetches all GitHub repositories owned by account and registers any
// that are not yet in the app as remote-only entries (LocalPath = "", no pull/download).
func (a *App) importAccountRemoteRepos(account config.Account) {
	if a.cfg == nil || a.github == nil || account.ID == "" {
		return
	}
	if a.cfg.FindAccount(account.ID) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	remoteRepos, err := a.github.ListUserRepos(ctx, account.ID)
	if err != nil {
		if a.log != nil {
			a.log.Warn("githubapi", "could not list remote repos for @%s: %v", account.Username, err)
		}
		return
	}

	addedCount := 0
	for _, r := range remoteRepos {
		// Stop immediately if the account was removed while importing
		if a.cfg.FindAccount(account.ID) == nil {
			return
		}
		// Check both account-scoped and global repo name to prevent duplicate cards
		if existing := a.cfg.FindRepoByName(r.Name); existing != nil {
			// If an existing repo was found (e.g. from prior account session or local folder),
			// link it to this account ID if it was unlinked or orphaned
			if existing.AccountID != account.ID {
				existing.AccountID = account.ID
				_ = a.cfg.UpdateRepo(*existing)
			}
			continue
		}
		vis := "public"
		if r.Private {
			vis = "private"
		}
		branch := strings.TrimSpace(r.DefaultBranch)
		if branch == "" {
			branch = "main"
		}
		repo := config.RepoConfig{
			ID:         uuid.New().String(),
			LocalPath:  "", // Remote-only: shown in app, not pulled/downloaded locally
			RemoteName: r.Name,
			AccountID:  account.ID,
			Visibility: vis,
			Enabled:    false,
			Branch:     branch,
			CommitMsg:  "chore: auto-sync {{timestamp}}",
			CreatedAt:  r.UpdatedAt,
		}
		if err := a.cfg.AddRepo(repo); err == nil {
			if a.watchSvc != nil {
				a.watchSvc.AddRepo(&repo)
			}
			addedCount++
		}
	}

	if addedCount > 0 {
		a.emitEvent("config:updated", a.cfg.Config())
		if a.watchSvc != nil {
			a.emitEvent("repo:status", a.watchSvc.Statuses())
		}
		if a.log != nil {
			a.log.Info("app", "imported %d remote repositories from @%s (remote-only, not downloaded)", addedCount, account.Username)
		}
	}
}

// GetAccountRateLimit returns current API quota metrics for a stored account.
func (a *App) GetAccountRateLimit(accountID string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	info, err := a.github.GetAccountRateLimit(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"limit":     info.Limit,
		"remaining": info.Remaining,
		"resetAt":   info.ResetAt.Format(time.RFC3339),
		"used":      info.Used,
	}, nil
}

// AddAccount securely encrypts and stores a new GitHub account.
func (a *App) AddAccount(label, username, email, pat string) error {
	encPAT, err := a.cfg.EncryptPAT(pat)
	if err != nil {
		return fmt.Errorf("encrypt PAT: %w", err)
	}

	account := config.Account{
		ID:           uuid.New().String(),
		Label:        label,
		Username:     username,
		Email:        email,
		EncryptedPAT: encPAT,
		CreatedAt:    time.Now(),
	}

	if err := a.cfg.AddAccount(account); err != nil {
		return err
	}
	a.log.Info("app", "added account: %s (%s)", label, username)
	go a.importAccountRemoteRepos(account)
	return nil
}

// RemoveAccount deletes a GitHub account, its associated repositories, and purges cached clients
// in a single atomic configuration update.
func (a *App) RemoveAccount(accountID string) error {
	if a.github != nil {
		a.github.InvalidateClient(accountID)
	}
	if a.cfg == nil {
		return fmt.Errorf("configuration store unavailable")
	}

	removedRepoIDs, err := a.cfg.RemoveAccountAndRepos(accountID)
	if err != nil {
		return err
	}

	if a.watchSvc != nil {
		for _, repoID := range removedRepoIDs {
			a.watchSvc.RemoveRepo(repoID)
		}
	}

	a.emitEvent("config:updated", a.cfg.Config())
	if a.watchSvc != nil {
		a.emitEvent("repo:status", a.watchSvc.Statuses())
	}
	if a.log != nil {
		a.log.Info("app", "removed account %s (cleaned up %d repos)", accountID, len(removedRepoIDs))
	}
	return nil
}

// AddRepo registers a new local folder for synchronization.
func (a *App) AddRepo(localPath, repoName, accountID, visibility string) error {
	cleanedPath := filepath.Clean(strings.TrimSpace(localPath))
	// Validate local folder path
	info, err := os.Stat(cleanedPath)
	if err != nil {
		return fmt.Errorf("path does not exist: %s", cleanedPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", cleanedPath)
	}

	// Safety check: prevent watching drive roots or OS system directories
	if !isSafePath(cleanedPath) {
		return fmt.Errorf("path %q is a protected root or system directory — please select a project folder", cleanedPath)
	}

	if a.cfg.FindRepoByPath(cleanedPath) != nil {
		return fmt.Errorf("folder %q is already registered in GitSync", cleanedPath)
	}

	home, _ := os.UserHomeDir()
	backupDir := filepath.Join(home, ".gitsync", "backups", repoName)

	// If a remote-only entry for this account & repo name already exists, link the local folder to it
	if existing := a.cfg.FindRepoByAccountAndName(accountID, repoName); existing != nil && strings.TrimSpace(existing.LocalPath) == "" {
		updated := *existing
		updated.LocalPath = cleanedPath
		updated.Visibility = visibility
		updated.Enabled = true
		updated.BackupDir = backupDir
		if err := a.cfg.UpdateRepo(updated); err != nil {
			return err
		}
		a.watchSvc.AddRepo(&updated)
		a.emitEvent("config:updated", a.cfg.Config())
		a.log.Info("app", "linked local folder to existing remote repository: %s → %s", cleanedPath, repoName)
		return nil
	}

	repo := config.RepoConfig{
		ID:         uuid.New().String(),
		LocalPath:  cleanedPath,
		RemoteName: repoName,
		AccountID:  accountID,
		Visibility: visibility,
		Enabled:    true,
		Branch:     "main",
		CommitMsg:  "chore: auto-sync {{timestamp}}",
		BackupDir:  backupDir,
		CreatedAt:  time.Now(),
	}

	if err := a.cfg.AddRepo(repo); err != nil {
		return err
	}

	// Register with watcher service and broadcast updated config
	a.watchSvc.AddRepo(&repo)
	a.emitEvent("config:updated", a.cfg.Config())
	a.log.Info("app", "added repository: %s → %s", cleanedPath, repoName)
	return nil
}

// RemoveRepo unregisters a repository locally and deletes the repository from GitHub.
func (a *App) RemoveRepo(repoID string) error {
	return a.RemoveRepoWithOptions(repoID, true)
}

// RemoveRepoWithOptions unregisters a repository from GitSync, and optionally deletes it on GitHub.
func (a *App) RemoveRepoWithOptions(repoID string, deleteFromGitHub bool) error {
	var targetRepo *config.RepoConfig
	if a.cfg != nil {
		if r := a.cfg.FindRepo(repoID); r != nil {
			copied := *r
			targetRepo = &copied
		}
	}

	if deleteFromGitHub && targetRepo != nil && targetRepo.AccountID != "" && targetRepo.RemoteName != "" && a.github != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		delErr := a.github.DeleteRepo(ctx, targetRepo.AccountID, targetRepo.RemoteName)
		cancel()
		if delErr != nil {
			if a.log != nil {
				a.log.Warn("app", "[%s] failed to delete remote repository on GitHub: %v", targetRepo.RemoteName, delErr)
			}
			a.emitEvent("toast:notify", map[string]interface{}{
				"id":        fmt.Sprintf("del-warn-%s-%d", targetRepo.RemoteName, time.Now().UnixNano()),
				"type":      "error",
				"title":     "GitHub Delete Permission Required",
				"message":   fmt.Sprintf("[%s] Removed from app, but GitHub deletion failed (ensure token has 'delete_repo' scope — re-authenticate in Settings).", targetRepo.RemoteName),
				"repoName":  targetRepo.RemoteName,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})
		} else {
			a.emitEvent("toast:notify", map[string]interface{}{
				"id":        fmt.Sprintf("del-ok-%s-%d", targetRepo.RemoteName, time.Now().UnixNano()),
				"type":      "success",
				"title":     "Repository Deleted",
				"message":   fmt.Sprintf("[%s] Permanently deleted from GitSync and GitHub", targetRepo.RemoteName),
				"repoName":  targetRepo.RemoteName,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})
		}
	} else if !deleteFromGitHub && targetRepo != nil {
		a.emitEvent("toast:notify", map[string]interface{}{
			"id":        fmt.Sprintf("rem-local-%s-%d", targetRepo.RemoteName, time.Now().UnixNano()),
			"type":      "success",
			"title":     "Removed from GitSync",
			"message":   fmt.Sprintf("[%s] Unlinked from GitSync (kept safely on GitHub)", targetRepo.RemoteName),
			"repoName":  targetRepo.RemoteName,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}

	if a.watchSvc != nil {
		a.watchSvc.RemoveRepo(repoID)
	}
	if err := a.cfg.RemoveRepo(repoID); err != nil {
		return err
	}
	a.emitEvent("config:updated", a.cfg.Config())
	a.log.Info("app", "removed repository: %s (deleteFromGitHub=%v)", repoID, deleteFromGitHub)
	return nil
}

// CloneRemoteRepo clones or links a remote-only repository into a selected local directory and enables watching.
func (a *App) CloneRemoteRepo(repoID string, selectedDir string) error {
	cleanedSelected := filepath.Clean(strings.TrimSpace(selectedDir))
	if cleanedSelected == "" {
		return fmt.Errorf("please select a destination folder")
	}

	repo := a.cfg.FindRepo(repoID)
	if repo == nil {
		return fmt.Errorf("repository %q not found", repoID)
	}
	account := a.cfg.FindAccount(repo.AccountID)
	if account == nil {
		return fmt.Errorf("GitHub account for %q not found", repo.RemoteName)
	}

	// Determine target directory:
	// If selected folder already matches repo.RemoteName or is completely empty, use it directly;
	// otherwise create a subfolder named repo.RemoteName inside selectedDir.
	targetDir := cleanedSelected
	if !strings.EqualFold(filepath.Base(cleanedSelected), repo.RemoteName) {
		entries, err := os.ReadDir(cleanedSelected)
		if err == nil && len(entries) > 0 {
			// Check if it already contains a .git folder
			if _, gErr := os.Stat(filepath.Join(cleanedSelected, ".git")); gErr != nil {
				targetDir = filepath.Join(cleanedSelected, repo.RemoteName)
			}
		}
	}

	if !isSafePath(targetDir) {
		return fmt.Errorf("path %q is a protected root or system directory — please select a project folder", targetDir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := a.git.Clone(ctx, repo, account, a.github, targetDir); err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	updated := *repo
	updated.LocalPath = targetDir
	updated.Enabled = true
	if updated.BackupDir == "" {
		updated.BackupDir = filepath.Join(home, ".gitsync", "backups", repo.RemoteName)
	}
	if err := a.cfg.UpdateRepo(updated); err != nil {
		return err
	}

	if a.watchSvc != nil {
		a.watchSvc.WatchClonedRepo(&updated)
	}
	a.emitEvent("config:updated", a.cfg.Config())
	a.emitEvent("toast:notify", map[string]interface{}{
		"id":        fmt.Sprintf("clone-%s-%d", repo.RemoteName, time.Now().UnixNano()),
		"type":      "success",
		"title":     "Repository Cloned & Synced",
		"message":   fmt.Sprintf("[%s] Downloaded to %s and watching for changes", repo.RemoteName, targetDir),
		"repoName":  repo.RemoteName,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

// PullRepoNow manually fetches and rebases the latest changes from GitHub for a linked repository.
func (a *App) PullRepoNow(repoID string) error {
	if a.watchSvc == nil {
		return fmt.Errorf("watcher service unavailable")
	}
	return a.watchSvc.TriggerPull(repoID)
}

// GenerateRepoReadme scans a repository and generates a tailored README.md using the configured AI provider chain.
func (a *App) GenerateRepoReadme(repoID, style, customPrompt string) (map[string]interface{}, error) {
	if a.cfg == nil {
		return nil, fmt.Errorf("configuration store unavailable")
	}
	repo := a.cfg.FindRepo(repoID)
	if repo == nil {
		return nil, fmt.Errorf("repository %q not found", repoID)
	}
	localPath := strings.TrimSpace(repo.LocalPath)
	if localPath == "" {
		return nil, fmt.Errorf("please clone or link a local folder before generating a README")
	}

	account := a.cfg.FindAccount(repo.AccountID)
	owner := ""
	ghToken := ""
	if account != nil {
		owner = account.Username
		if a.github != nil {
			if pat, err := a.github.DecryptPAT(account.ID); err == nil {
				ghToken = pat
			}
		}
	}

	prefs := a.cfg.Config().Preferences
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()

	res, err := aireadme.Generate(ctx, aireadme.Request{
		LocalPath:    localPath,
		RepoName:     repo.RemoteName,
		Owner:        owner,
		Branch:       repo.Branch,
		Visibility:   repo.Visibility,
		Style:        style,
		CustomPrompt: customPrompt,
		Provider:     prefs.AIProvider,
		APIKey:       prefs.AIApiKey,
		Model:        prefs.AIModel,
		BaseURL:      prefs.AIBaseURL,
		GitHubToken:  ghToken,
	})
	if err != nil {
		return nil, err
	}

	if a.log != nil {
		a.log.Info("ai", "[%s] generated README.md via %s", repo.RemoteName, res.Provider)
	}
	return map[string]interface{}{
		"content":      res.Content,
		"markdown":     res.Markdown,
		"provider":     res.Provider,
		"tech_stack":   res.TechStack,
		"has_existing": res.HasExisting,
	}, nil
}

// FetchAIModels returns the list of available models for the selected AI provider.
// When GitHub is selected and apiKey is empty, it uses the first connected GitHub account's token.
func (a *App) FetchAIModels(provider, apiKey, baseURL string) ([]string, error) {
	ghToken := ""
	if a.cfg != nil && a.github != nil {
		accounts := a.cfg.Config().Accounts
		for _, acc := range accounts {
			if pat, err := a.github.DecryptPAT(acc.ID); err == nil && strings.TrimSpace(pat) != "" {
				ghToken = pat
				break
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return aireadme.ListModels(ctx, provider, apiKey, baseURL, ghToken)
}

// SaveRepoReadme writes the generated/edited README.md to the repository's local folder and optionally triggers a sync.
func (a *App) SaveRepoReadme(repoID, content string, syncNow bool) error {
	if a.cfg == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	repo := a.cfg.FindRepo(repoID)
	if repo == nil {
		return fmt.Errorf("repository %q not found", repoID)
	}
	localPath := strings.TrimSpace(repo.LocalPath)
	if localPath == "" {
		return fmt.Errorf("repository has no local folder")
	}

	readmePath := filepath.Join(localPath, "README.md")
	if err := os.WriteFile(readmePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write README.md: %w", err)
	}

	if a.log != nil {
		a.log.Info("ai", "[%s] saved README.md to %s (syncNow=%v)", repo.RemoteName, readmePath, syncNow)
	}

	a.emitEvent("toast:notify", map[string]interface{}{
		"id":        fmt.Sprintf("readme-%s-%d", repo.RemoteName, time.Now().UnixNano()),
		"type":      "success",
		"title":     "README.md Saved",
		"message":   fmt.Sprintf("[%s] README.md written to %s", repo.RemoteName, readmePath),
		"repoName":  repo.RemoteName,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	if syncNow && a.watchSvc != nil {
		_ = a.watchSvc.TriggerSync(repoID)
	}
	return nil
}

// ToggleRepo toggles sync enabled status for a repository.
func (a *App) ToggleRepo(repoID string, enabled bool) error {
	repo := a.cfg.FindRepo(repoID)
	if repo == nil {
		return fmt.Errorf("repo %q not found", repoID)
	}
	repo.Enabled = enabled
	if err := a.cfg.UpdateRepo(*repo); err != nil {
		return err
	}

	if enabled {
		a.watchSvc.AddRepo(repo)
	} else {
		a.watchSvc.PauseRepo(repoID)
	}
	a.emitEvent("config:updated", a.cfg.Config())
	return nil
}

// UpdateRepoSettings updates per-repository branch, commit message template, and linked GitHub account settings.
func (a *App) UpdateRepoSettings(repoID, branch, commitMsg, accountID string) error {
	repo := a.cfg.FindRepo(repoID)
	if repo == nil {
		return fmt.Errorf("repo %q not found", repoID)
	}
	cleanBranch := strings.TrimSpace(branch)
	if cleanBranch == "" {
		cleanBranch = "main"
	}
	cleanCommitMsg := strings.TrimSpace(commitMsg)
	if cleanCommitMsg == "" {
		cleanCommitMsg = "chore: auto-sync {{timestamp}}"
	}

	branchChanged := repo.Branch != cleanBranch
	repo.Branch = cleanBranch
	repo.CommitMsg = cleanCommitMsg

	cleanAccountID := strings.TrimSpace(accountID)
	if cleanAccountID != "" && a.cfg.FindAccount(cleanAccountID) != nil {
		repo.AccountID = cleanAccountID
	}

	if err := a.cfg.UpdateRepo(*repo); err != nil {
		return err
	}

	a.emitEvent("config:updated", a.cfg.Config())
	if a.log != nil {
		a.log.Info("app", "[%s] updated settings (branch=%s, commitMsg=%s, account=%s)", repo.RemoteName, repo.Branch, repo.CommitMsg, repo.AccountID)
	}

	// If the branch changed and a local folder is linked, trigger a sync immediately
	// so the local branch is switched/created and pushed to GitHub right away.
	if branchChanged && strings.TrimSpace(repo.LocalPath) != "" && a.watchSvc != nil {
		_ = a.watchSvc.TriggerSync(repoID)
	}
	return nil
}

// UpdatePreferences persists updated global preferences.
func (a *App) UpdatePreferences(prefs config.Preferences) error {
	oldPrefs := a.cfg.Config().Preferences
	if prefs.GitPath != oldPrefs.GitPath {
		a.git = gitengine.New(a.log, prefs.GitPath)
		a.git.SetConfigStore(a.cfg)
		if a.watchSvc != nil {
			a.watchSvc.SetGitEngine(a.git)
		}
	}
	if prefs.LaunchOnBoot != oldPrefs.LaunchOnBoot {
		if err := autostart.Set(prefs.LaunchOnBoot); err != nil && a.log != nil {
			a.log.Warn("autostart", "failed to sync startup registry key: %v", err)
		}
	}
	if err := a.cfg.UpdatePreferences(prefs); err != nil {
		return err
	}
	if a.watchSvc != nil {
		a.watchSvc.UpdateDebounceIntervals(prefs.DebounceSec, prefs.MaxDebounceSec)
	}
	a.emitEvent("config:updated", a.cfg.Config())
	return nil
}

// SetAutoStart enables or disables launching GitSync automatically on Windows startup
// via HKCU\Software\Microsoft\Windows\CurrentVersion\Run and persists LaunchOnBoot.
func (a *App) SetAutoStart(enabled bool) error {
	if err := autostart.Set(enabled); err != nil {
		if a.log != nil {
			a.log.Error("autostart", "failed to set auto-start (%v): %v", enabled, err)
		}
		return err
	}
	if a.cfg != nil {
		prefs := a.cfg.Config().Preferences
		prefs.LaunchOnBoot = enabled
		if err := a.cfg.UpdatePreferences(prefs); err != nil {
			return err
		}
		a.emitEvent("config:updated", a.cfg.Config())
	}
	if a.log != nil {
		a.log.Info("autostart", "launch on system startup set to %v", enabled)
	}
	return nil
}

// SelectDirectory opens a native folder selection dialog with panic recovery and graceful error handling.
// It checks for a.ctx == nil and wraps runtime.OpenDirectoryDialog with a robust panic recovery block
// to prevent native OS window pointer crashes in sandboxed or restricted environments.
func (a *App) SelectDirectory() (resPath string, err error) {
	defer func() {
		if r := recover(); r != nil {
			if a != nil && a.log != nil {
				a.log.Warn("dialog", "recovered from SelectDirectory panic: %v", r)
			}
			resPath = ""
			err = nil
		}
	}()

	// 1. Check if a.ctx is nil
	if a == nil || a.ctx == nil {
		if a != nil && a.log != nil {
			a.log.Warn("dialog", "cannot open folder dialog: runtime context is nil")
		}
		return "", fmt.Errorf("application runtime context is not ready")
	}

	// 2. On Windows, use the safe out-of-process folder dialog.
	// In Windows Sandbox, WDAGUtilityAccount, and frameless Wails windows, in-process
	// COM IFileDialog (wailsRuntime.OpenDirectoryDialog) triggers unrecoverable native
	// 0xc0000005 access violations that terminate the process.
	// The out-of-process dialog runs isolated, can never crash GitSync, and works in all Windows environments.
	if stdRuntime.GOOS == "windows" {
		if a != nil && a.log != nil {
			a.log.Info("dialog", "prompting Windows folder dialog")
		}
		path := openSafeDirectoryDialog()
		return path, nil
	}

	// 3. For non-Windows platforms (macOS / Linux), wrap Wails native directory dialog in a safe goroutine with panic recovery
	type dialogResult struct {
		path string
		err  error
	}

	done := make(chan dialogResult, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				if a != nil && a.log != nil {
					a.log.Warn("dialog", "recovered from native folder dialog goroutine panic: %v", r)
				}
				done <- dialogResult{path: "", err: nil}
			}
		}()

		path, dlgErr := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
			Title: "Select Folder to Watch",
		})
		if dlgErr != nil {
			if a != nil && a.log != nil {
				a.log.Warn("dialog", "native folder dialog failed or was cancelled: %v", dlgErr)
			}
			done <- dialogResult{path: "", err: nil}
			return
		}

		done <- dialogResult{path: path, err: nil}
	}()

	select {
	case res := <-done:
		return res.path, res.err
	case <-time.After(3 * time.Minute):
		if a != nil && a.log != nil {
			a.log.Warn("dialog", "native folder dialog timed out waiting for user input")
		}
		return "", nil
	}
}

// OpenFolderDialog is an alias to SelectDirectory for backwards compatibility.
func (a *App) OpenFolderDialog() (string, error) {
	return a.SelectDirectory()
}

// openSafeDirectoryDialog provides an out-of-process Windows folder picker fallback
// using PowerShell System.Windows.Forms.FolderBrowserDialog when in-process COM dialog crashes or fails.
func openSafeDirectoryDialog() string {
	if stdRuntime.GOOS != "windows" {
		return ""
	}
	psScript := `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.FolderBrowserDialog; $f.Description = 'Select Folder to Watch'; $f.ShowNewFolderButton = $true; $top = New-Object System.Windows.Forms.Form; $top.TopMost = $true; if ($f.ShowDialog($top) -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($f.SelectedPath) }; $top.Dispose()`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CheckDependencies checks whether Git is missing on the machine.
// Returns (missing bool, details string).
func (a *App) CheckDependencies() (bool, string) {
	customPath := ""
	if a.cfg != nil {
		customPath = a.cfg.Config().Preferences.GitPath
	}
	return gitengine.CheckMissingDependencies(customPath)
}

// InstallDependencies downloads, extracts, and configures standalone portable Git,
// emitting real-time progress events over dependency:progress.
func (a *App) InstallDependencies() error {
	a.emitEvent("dependency:progress", "Initializing dependency installer...")

	progressCb := func(status string) {
		if a.log != nil {
			a.log.Info("installer", status)
		}
		a.emitEvent("dependency:progress", status)
	}

	if err := gitengine.InstallPortableGit(progressCb); err != nil {
		if a.log != nil {
			a.log.Error("installer", "Failed to install portable Git: %v", err)
		}
		return err
	}

	// Update git engine with the newly configured portable Git path
	gitPath := ""
	if a.cfg != nil {
		gitPath = a.cfg.Config().Preferences.GitPath
	}
	if gitPath == "" {
		if targetExe, err := gitengine.GetPortableGitBinary(); err == nil {
			gitPath = targetExe
		}
	}

	a.git = gitengine.New(a.log, gitPath)
	a.git.SetConfigStore(a.cfg)
	if a.watchSvc != nil {
		a.watchSvc.SetGitEngine(a.git)
	}

	if a.cfg != nil {
		prefs := a.cfg.Config().Preferences
		prefs.GitPath = gitPath
		_ = a.cfg.UpdatePreferences(prefs)
		a.emitEvent("config:updated", a.cfg.Config())
	}

	a.emitEvent("app:ready", map[string]interface{}{
		"gitAvailable": a.git.Available(),
		"gitPath":      a.git.GitPath(),
		"version":      "1.0.0",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	})

	if a.log != nil {
		a.log.Info("installer", "Portable Git successfully configured at: %s", a.git.GitPath())
	}

	return nil
}

// CheckForUpdates triggers an immediate check against GitHub Releases.
func (a *App) CheckForUpdates() (*updater.UpdateInfo, error) {
	if a.updater == nil {
		return nil, fmt.Errorf("updater service unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return a.updater.Check(ctx)
}

// GetUpdateInfo returns current cached update metadata.
func (a *App) GetUpdateInfo() *updater.UpdateInfo {
	if a.updater == nil {
		return &updater.UpdateInfo{
			CurrentVersion: updater.CurrentVersion,
			LatestVersion:  updater.CurrentVersion,
			HasUpdate:      false,
		}
	}
	return a.updater.GetInfo()
}

// InstallUpdate downloads the latest release binary, stages it, spawns the replacement script,
// and exits the application cleanly so the updated executable can launch.
func (a *App) InstallUpdate() error {
	if a.updater == nil {
		return fmt.Errorf("updater service unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	err := a.updater.InstallUpdate(ctx, func(pct int, status string) {
		if a.log != nil {
			a.log.Info("updater", "[%d%%] %s", pct, status)
		}
	})
	if err != nil {
		if a.log != nil {
			a.log.Error("updater", "install update failed: %v", err)
		}
		return err
	}

	// Schedule clean application termination so the background script can overwrite the binary
	go func() {
		time.Sleep(300 * time.Millisecond)
		a.QuitApp()
	}()
	return nil
}

// OpenExternalURL opens a URL in the user's default browser.
func (a *App) OpenExternalURL(url string) {
	if a.ctx != nil && url != "" {
		wailsRuntime.BrowserOpenURL(a.ctx, url)
	}
}

// OpenLogsFolder opens ~/.gitsync/logs/ in the system file explorer.
func (a *App) OpenLogsFolder() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".gitsync", "logs")
	_ = os.MkdirAll(dir, 0755)
	return openLocalExplorer(dir)
}

// OpenBackupsFolder opens ~/.gitsync/backups/ in the system file explorer.
func (a *App) OpenBackupsFolder() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".gitsync", "backups")
	if a.cfg != nil && strings.TrimSpace(a.cfg.Config().Preferences.BackupDir) != "" {
		dir = strings.TrimSpace(a.cfg.Config().Preferences.BackupDir)
	}
	_ = os.MkdirAll(dir, 0755)
	return openLocalExplorer(dir)
}

// OpenLocalFolder opens a local directory path in the system file explorer.
func (a *App) OpenLocalFolder(localPath string) error {
	trimmed := strings.TrimSpace(localPath)
	if trimmed == "" {
		return fmt.Errorf("folder path is empty")
	}
	return openLocalExplorer(trimmed)
}

// OpenRepoBackups opens the per-repository backup snapshots folder in the system file explorer.
func (a *App) OpenRepoBackups(repoName string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	baseDir := filepath.Join(home, ".gitsync", "backups")
	if a.cfg != nil && strings.TrimSpace(a.cfg.Config().Preferences.BackupDir) != "" {
		baseDir = strings.TrimSpace(a.cfg.Config().Preferences.BackupDir)
	}
	target := baseDir
	if strings.TrimSpace(repoName) != "" {
		target = filepath.Join(baseDir, strings.TrimSpace(repoName))
	}
	_ = os.MkdirAll(target, 0755)
	return openLocalExplorer(target)
}

// OpenInVSCode opens a local folder path in Visual Studio Code.
func (a *App) OpenInVSCode(localPath string) error {
	trimmed := filepath.Clean(strings.TrimSpace(localPath))
	if trimmed == "" {
		return fmt.Errorf("folder path is empty")
	}
	if stdRuntime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "code", trimmed)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000,
		}
		return cmd.Start()
	}
	return exec.Command("code", trimmed).Start()
}

// OpenInTerminal opens a terminal window at the specified local folder path.
func (a *App) OpenInTerminal(localPath string) error {
	trimmed := filepath.Clean(strings.TrimSpace(localPath))
	if trimmed == "" {
		return fmt.Errorf("folder path is empty")
	}
	if stdRuntime.GOOS == "windows" {
		// Prefer Windows Terminal (wt.exe) if available, otherwise launch PowerShell
		if _, err := exec.LookPath("wt.exe"); err == nil {
			return exec.Command("wt.exe", "-d", trimmed).Start()
		}
		return exec.Command("cmd", "/c", "start", "powershell", "-NoExit", "-Command", fmt.Sprintf("Set-Location -LiteralPath '%s'", strings.ReplaceAll(trimmed, "'", "''"))).Start()
	}
	return openLocalExplorer(trimmed)
}

func openLocalExplorer(dir string) error {
	cleaned := filepath.Clean(dir)
	var cmd *exec.Cmd
	switch stdRuntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", cleaned)
	case "darwin":
		cmd = exec.Command("open", cleaned)
	default:
		cmd = exec.Command("xdg-open", cleaned)
	}
	return cmd.Start()
}

// InspectLocalFolder checks a local folder path for existence, base directory name,
// and whether an existing .git repository is present.
func (a *App) InspectLocalFolder(localPath string) map[string]interface{} {
	raw := strings.TrimSpace(localPath)
	if raw == "" {
		return map[string]interface{}{
			"exists":   false,
			"isDir":    false,
			"hasGit":   false,
			"baseName": "",
		}
	}
	cleaned := filepath.Clean(raw)
	baseName := filepath.Base(cleaned)
	info, err := os.Stat(cleaned)
	if err != nil {
		return map[string]interface{}{
			"exists":   false,
			"isDir":    false,
			"hasGit":   false,
			"baseName": baseName,
		}
	}
	isDir := info.IsDir()
	hasGit := false
	if isDir {
		gitPath := filepath.Join(cleaned, ".git")
		if _, gErr := os.Stat(gitPath); gErr == nil {
			hasGit = true
		}
	}
	return map[string]interface{}{
		"exists":   true,
		"isDir":    isDir,
		"hasGit":   hasGit,
		"baseName": baseName,
	}
}

// GetSystemInfo returns diagnostic information for the environment.
func (a *App) GetSystemInfo() map[string]interface{} {
	hostname, _ := os.Hostname()
	return map[string]interface{}{
		"gitAvailable": a.git.Available(),
		"gitPath":      a.git.GitPath(),
		"hostname":     hostname,
		"version":      "1.0.0",
		"time":         time.Now().UTC().Format(time.RFC3339),
	}
}

// ─── Internal Helpers ─────────────────────────────────────────────────────────

// cleanupStaleIndexLocks checks all watched repositories for leftover .git/index.lock files
// from system crashes or forced process kills, removing them safely on startup.
func (a *App) cleanupStaleIndexLocks() {
	cfg := a.cfg.Config()
	for _, r := range cfg.Repos {
		lockPath := filepath.Join(r.LocalPath, ".git", "index.lock")
		if info, err := os.Stat(lockPath); err == nil {
			// If file exists and was modified more than 5 seconds ago, it is a dead lock
			if time.Since(info.ModTime()) > 5*time.Second {
				if remErr := os.Remove(lockPath); remErr == nil {
					if a.log != nil {
						a.log.Warn("app", "[%s] removed stale .git/index.lock from previous crash", r.RemoteName)
					}
				}
			}
		}
	}
}

// isSafePath prevents users from accidentally registering drive roots or critical OS directories.
func isSafePath(path string) bool {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)

	// Reject filesystem / drive roots (e.g. "C:\", "X:\", "/")
	parts := 0
	for p := abs; p != filepath.Dir(p); p = filepath.Dir(p) {
		parts++
	}
	if parts < 1 {
		return false
	}

	// Reject the user's home directory root itself
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if strings.EqualFold(abs, filepath.Clean(home)) {
			return false
		}
	}

	// Reject critical top-level OS / system directories
	norm := strings.ToLower(filepath.ToSlash(abs))
	if len(norm) >= 3 && norm[1] == ':' && norm[2] == '/' {
		rel := norm[3:]
		switch rel {
		case "windows", "program files", "program files (x86)", "programdata",
			"users", "$recycle.bin", "system volume information", "recovery":
			return false
		}
	} else {
		switch norm {
		case "/bin", "/sbin", "/usr", "/etc", "/var", "/proc", "/sys", "/dev",
			"/system", "/library", "/users", "/home", "/root":
			return false
		}
	}

	return true
}
