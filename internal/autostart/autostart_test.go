package autostart

import (
	"testing"
)

func TestAutoStartSetAndCheck(t *testing.T) {
	orig, _ := IsEnabled()
	defer func() {
		_ = Set(orig)
	}()

	// Enable with a test executable path
	if err := Set(true, `C:\Program Files\GitSync\GitSync.exe`); err != nil {
		t.Fatalf("Set(true) failed: %v", err)
	}

	enabled, err := IsEnabled()
	if err != nil {
		t.Fatalf("IsEnabled() after Set(true) failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected IsEnabled() to be true after Set(true)")
	}

	// Disable
	if err := Set(false); err != nil {
		t.Fatalf("Set(false) failed: %v", err)
	}

	enabledAfter, err := IsEnabled()
	if err != nil {
		t.Fatalf("IsEnabled() after Set(false) failed: %v", err)
	}
	if enabledAfter {
		t.Errorf("expected IsEnabled() to be false after Set(false)")
	}
}

