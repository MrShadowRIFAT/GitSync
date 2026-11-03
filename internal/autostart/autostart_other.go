//go:build !windows

package autostart

import "sync"

var (
	mu          sync.RWMutex
	mockEnabled bool
)

// Set is a no-op stub for non-Windows platforms.
func Set(enabled bool, _ ...string) error {
	mu.Lock()
	mockEnabled = enabled
	mu.Unlock()
	return nil
}

// IsEnabled reports the in-memory autostart state on non-Windows platforms.
func IsEnabled() (bool, error) {
	mu.RLock()
	defer mu.RUnlock()
	return mockEnabled, nil
}

// RegisterProtocolHandler is a no-op stub on non-Windows platforms.
func RegisterProtocolHandler(_ ...string) error {
	return nil
}


