package tray

import (
	"strings"
	"testing"

	"github.com/user/gitsync/internal/config"
)

func TestFormatTooltipAndStatus(t *testing.T) {
	tests := []struct {
		name              string
		metrics           Metrics
		wantTooltipSubstr string
		wantStatusSubstr  string
	}{
		{
			name: "paused state takes priority",
			metrics: Metrics{
				TotalRepos:   3,
				ActiveRepos:  2,
				PendingSyncs: 5,
				Conflicts:    1,
				IsPaused:     true,
			},
			wantTooltipSubstr: "GitSync (Paused)",
			wantStatusSubstr:  "Status: Paused",
		},
		{
			name: "conflict state",
			metrics: Metrics{
				TotalRepos:   3,
				ActiveRepos:  3,
				PendingSyncs: 1,
				Conflicts:    2,
				IsPaused:     false,
			},
			wantTooltipSubstr: "CONFLICTS",
			wantStatusSubstr:  "2 conflicts detected",
		},
		{
			name: "syncing state",
			metrics: Metrics{
				TotalRepos:   3,
				ActiveRepos:  3,
				Syncing:      1,
				PendingSyncs: 0,
				Conflicts:    0,
				IsPaused:     false,
			},
			wantTooltipSubstr: "Syncing in progress",
			wantStatusSubstr:  "Status: Syncing in progress",
		},
		{
			name: "pending changes state",
			metrics: Metrics{
				TotalRepos:   3,
				ActiveRepos:  2,
				PendingSyncs: 4,
				Conflicts:    0,
				IsPaused:     false,
			},
			wantTooltipSubstr: "4 pending changes",
			wantStatusSubstr:  "Status: 4 pending changes",
		},
		{
			name: "no active repos",
			metrics: Metrics{
				TotalRepos:   1,
				ActiveRepos:  0,
				PendingSyncs: 0,
				Conflicts:    0,
				IsPaused:     false,
			},
			wantTooltipSubstr: "No active repositories",
			wantStatusSubstr:  "Status: No active repositories",
		},
		{
			name: "all synced clean state",
			metrics: Metrics{
				TotalRepos:   2,
				ActiveRepos:  2,
				PendingSyncs: 0,
				Conflicts:    0,
				IsPaused:     false,
			},
			wantTooltipSubstr: "All synced",
			wantStatusSubstr:  "All 2 repos synced",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tooltip, status := FormatTooltipAndStatus(tc.metrics)
			if !strings.Contains(tooltip, tc.wantTooltipSubstr) {
				t.Errorf("tooltip %q does not contain %q", tooltip, tc.wantTooltipSubstr)
			}
			if !strings.Contains(status, tc.wantStatusSubstr) {
				t.Errorf("status %q does not contain %q", status, tc.wantStatusSubstr)
			}
		})
	}
}

func TestToggleGlobalPauseEmitsEvent(t *testing.T) {
	cfgStore, err := config.NewStore()
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	origPause := cfgStore.Config().Preferences.GlobalPause
	defer func() {
		prefs := cfgStore.Config().Preferences
		prefs.GlobalPause = origPause
		_ = cfgStore.UpdatePreferences(prefs)
	}()

	var emittedEvent string
	var emittedVal bool
	m := &Manager{
		cfg: cfgStore,
		emit: func(name string, data interface{}) {
			emittedEvent = name
			if b, ok := data.(bool); ok {
				emittedVal = b
			}
		},
	}

	m.toggleGlobalPause()

	if emittedEvent != "global_pause:changed" {
		t.Errorf("expected event 'global_pause:changed', got %q", emittedEvent)
	}
	if emittedVal != !origPause {
		t.Errorf("expected emitted value %v, got %v", !origPause, emittedVal)
	}
}

func TestPauseMenuTitleAndTriggerSyncAll(t *testing.T) {
	activeTitle, _ := PauseMenuTitle(false)
	if activeTitle != "Pause All Syncing" {
		t.Errorf("expected 'Pause All Syncing', got %q", activeTitle)
	}

	pausedTitle, _ := PauseMenuTitle(true)
	if pausedTitle != "Resume Syncing" {
		t.Errorf("expected 'Resume Syncing', got %q", pausedTitle)
	}

	synced := false
	m := &Manager{
		syncAll: func() {
			synced = true
		},
	}
	m.triggerSyncAll()
	if !synced {
		t.Errorf("expected triggerSyncAll to invoke syncAll callback")
	}
}


