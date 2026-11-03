//go:build !windows

package config

import (
	"os"
	"strings"
)

// getPlatformMachineID retrieves standard machine ID on Linux/macOS.
func getPlatformMachineID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(path); err == nil {
			trimmed := strings.TrimSpace(string(data))
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
