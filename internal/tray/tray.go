package tray

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/getlantern/systray"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/user/gitsync/internal/config"
	"github.com/user/gitsync/internal/logger"
)

//go:embed icons/icon_active.ico
var iconActive []byte

//go:embed icons/icon_paused.ico
var iconPaused []byte

// Metrics describes current live sync metrics across all workspaces.
type Metrics struct {
	TotalRepos   int  `json:"total_repos"`
	ActiveRepos  int  `json:"active_repos"`
	PendingSyncs int  `json:"pending_syncs"`
	Conflicts    int  `json:"conflicts"`
	Syncing      int  `json:"syncing"`
	IsPaused     bool `json:"is_paused"`
}

var (
	metricsMu   sync.RWMutex
	lastMetrics Metrics
	mStatusItem *systray.MenuItem
	mPauseItem  *systray.MenuItem
	isReady     bool
	readyMu     sync.RWMutex
)

// FormatTooltipAndStatus formats the hover tooltip and status menu title for the given metrics.
func FormatTooltipAndStatus(m Metrics) (string, string) {
	if m.IsPaused {
		tooltip := fmt.Sprintf("GitSync (Paused) — %d repos (%d active)", m.TotalRepos, m.ActiveRepos)
		status := fmt.Sprintf("Status: Paused (%d active repos)", m.ActiveRepos)
		return tooltip, status
	}

	if m.Conflicts > 0 {
		conflictWord := "conflict"
		if m.Conflicts > 1 {
			conflictWord = "conflicts"
		}
		tooltip := fmt.Sprintf("GitSync — %d active repos • %d pending • %d %s",
			m.ActiveRepos, m.PendingSyncs, m.Conflicts, strings.ToUpper(conflictWord))
		status := fmt.Sprintf("Status: %d %s detected!", m.Conflicts, conflictWord)
		return tooltip, status
	}

	if m.Syncing > 0 {
		tooltip := fmt.Sprintf("GitSync — Syncing in progress (%d active repos)", m.ActiveRepos)
		status := "Status: Syncing in progress..."
		return tooltip, status
	}

	if m.PendingSyncs > 0 {
		pendingWord := "pending change"
		if m.PendingSyncs > 1 {
			pendingWord = "pending changes"
		}
		tooltip := fmt.Sprintf("GitSync — %d active repos • %d %s", m.ActiveRepos, m.PendingSyncs, pendingWord)
		status := fmt.Sprintf("Status: %d %s", m.PendingSyncs, pendingWord)
		return tooltip, status
	}

	if m.ActiveRepos == 0 {
		tooltip := "GitSync — No active repositories"
		status := "Status: No active repositories"
		return tooltip, status
	}

	repoWord := "repos"
	if m.ActiveRepos == 1 {
		repoWord = "repo"
	}
	tooltip := fmt.Sprintf("GitSync — %d active %s • All synced", m.ActiveRepos, repoWord)
	status := fmt.Sprintf("Status: All %d %s synced", m.ActiveRepos, repoWord)
	return tooltip, status
}

// PauseMenuTitle returns the dynamic menu label and tooltip for the pause/resume tray item.
func PauseMenuTitle(paused bool) (string, string) {
	if paused {
		return "Resume Syncing", "Resume automatic background syncing"
	}
	return "Pause All Syncing", "Temporarily pause all automatic syncing"
}

// Manager holds references needed for tray menu actions.
type Manager struct {
	ctx     context.Context
	cfg     *config.Store
	log     *logger.Logger
	show    func()
	hide    func()
	quit    func()
	emit    func(string, interface{})
	syncAll func()
}

// Run starts the system tray (blocking). Call this in a goroutine.
func Run(ctx context.Context, cfg *config.Store, log *logger.Logger, show, hide, quit func(), emit func(string, interface{}), syncAll ...func()) {
	var syncAllFn func()
	if len(syncAll) > 0 {
		syncAllFn = syncAll[0]
	}
	m := &Manager{ctx: ctx, cfg: cfg, log: log, show: show, hide: hide, quit: quit, emit: emit, syncAll: syncAllFn}
	systray.Run(m.onReady, m.onExit)
}

func (m *Manager) onReady() {
	systray.SetIcon(iconActive)
	systray.SetTitle("GitSync")

	mStatus := systray.AddMenuItem("GitSync — Initializing...", "Live synchronization status")
	mStatus.Disable()
	systray.AddSeparator()

	mOpen := systray.AddMenuItem("Open Dashboard", "Show the GitSync window")
	mSyncAll := systray.AddMenuItem("Sync All Now", "Trigger an immediate sync for all active repositories")

	initialPaused := m.cfg.Config().Preferences.GlobalPause
	pauseTitle, pauseTooltip := PauseMenuTitle(initialPaused)
	mPause := systray.AddMenuItemCheckbox(
		pauseTitle,
		pauseTooltip,
		initialPaused,
	)
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit GitSync", "Exit the application completely")

	readyMu.Lock()
	mStatusItem = mStatus
	mPauseItem = mPause
	isReady = true
	readyMu.Unlock()

	// Initial metrics push
	metricsMu.RLock()
	cur := lastMetrics
	metricsMu.RUnlock()
	cur.IsPaused = initialPaused
	UpdateMetrics(cur)

	go m.menuLoop(mOpen, mSyncAll, mPause, mQuit)
}

func (m *Manager) menuLoop(mOpen, mSyncAll, mPause, mQuit *systray.MenuItem) {
	for {
		select {
		case <-mOpen.ClickedCh:
			m.show()

		case <-mSyncAll.ClickedCh:
			m.triggerSyncAll()

		case <-mPause.ClickedCh:
			m.toggleGlobalPause()

		case <-mQuit.ClickedCh:
			m.log.Info("tray", "quit requested from tray")
			systray.Quit()
			m.quit()
			return
		}
	}
}

func (m *Manager) triggerSyncAll() {
	if m.log != nil {
		m.log.Info("tray", "sync all repositories triggered from tray")
	}
	if m.syncAll != nil {
		m.syncAll()
	}
}

func (m *Manager) toggleGlobalPause() {
	prefs := m.cfg.Config().Preferences
	prefs.GlobalPause = !prefs.GlobalPause
	_ = m.cfg.UpdatePreferences(prefs)

	UpdatePauseState(prefs.GlobalPause)

	if m.emit != nil {
		m.emit("global_pause:changed", prefs.GlobalPause)
	} else if m.ctx != nil {
		func() {
			defer func() { _ = recover() }()
			wailsRuntime.EventsEmit(m.ctx, "global_pause:changed", prefs.GlobalPause)
		}()
	}

	if m.log != nil {
		if prefs.GlobalPause {
			m.log.Info("tray", "global sync paused by user from tray")
		} else {
			m.log.Info("tray", "global sync resumed by user from tray")
		}
	}
}

func (m *Manager) onExit() {
	m.log.Info("tray", "tray exited")
}

// UpdateMetrics updates tray icon, tooltip, and status menu item with current metrics.
func UpdateMetrics(m Metrics) {
	metricsMu.Lock()
	lastMetrics = m
	metricsMu.Unlock()

	readyMu.RLock()
	ready := isReady
	statusItem := mStatusItem
	pauseItem := mPauseItem
	readyMu.RUnlock()

	if !ready {
		return
	}

	tooltip, menuStatus := FormatTooltipAndStatus(m)
	systray.SetTooltip(tooltip)
	if statusItem != nil {
		statusItem.SetTitle(menuStatus)
	}

	if m.IsPaused {
		systray.SetIcon(iconPaused)
		if pauseItem != nil {
			title, tip := PauseMenuTitle(true)
			pauseItem.SetTitle(title)
			pauseItem.SetTooltip(tip)
			pauseItem.Check()
		}
	} else {
		systray.SetIcon(iconActive)
		if pauseItem != nil {
			title, tip := PauseMenuTitle(false)
			pauseItem.SetTitle(title)
			pauseItem.SetTooltip(tip)
			pauseItem.Uncheck()
		}
	}
}

// UpdatePauseState syncs the tray icon and tooltip when global pause toggles.
func UpdatePauseState(paused bool) {
	metricsMu.Lock()
	lastMetrics.IsPaused = paused
	m := lastMetrics
	metricsMu.Unlock()

	UpdateMetrics(m)
}
