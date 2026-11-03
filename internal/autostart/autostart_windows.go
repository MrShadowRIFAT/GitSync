//go:build windows

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	appValueName    = "GitSync"
)

// Set enables or disables launching GitSync automatically on Windows user login
// by managing HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
func Set(enabled bool, customExePath ...string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runRegistryPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("open startup registry key: %w", err)
	}
	defer k.Close()

	if !enabled {
		err = k.DeleteValue(appValueName)
		if err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("remove startup registry value: %w", err)
		}
		return nil
	}

	exePath := ""
	if len(customExePath) > 0 && strings.TrimSpace(customExePath[0]) != "" {
		exePath = strings.TrimSpace(customExePath[0])
	} else {
		resolved, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable path: %w", err)
		}
		exePath = resolved
	}

	if abs, err := filepath.Abs(exePath); err == nil {
		exePath = abs
	}

	cmdValue := fmt.Sprintf(`"%s" --minimized`, exePath)
	if err := k.SetStringValue(appValueName, cmdValue); err != nil {
		return fmt.Errorf("set startup registry value: %w", err)
	}
	return nil
}

// IsEnabled checks whether the GitSync startup value exists in HKCU\...\Run.
func IsEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	defer k.Close()

	val, _, err := k.GetStringValue(appValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(val) != "", nil
}

// RegisterProtocolHandler registers the custom "gitsync://" URI scheme in HKCU\Software\Classes\gitsync
// so that web browsers prompt "Open GitSync?" after OAuth authentication and focus the desktop app.
func RegisterProtocolHandler(customExePath ...string) error {
	exePath := ""
	if len(customExePath) > 0 && strings.TrimSpace(customExePath[0]) != "" {
		exePath = strings.TrimSpace(customExePath[0])
	} else {
		resolved, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable path: %w", err)
		}
		exePath = resolved
	}
	if abs, err := filepath.Abs(exePath); err == nil {
		exePath = abs
	}

	baseKeyPath := `Software\Classes\gitsync`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, baseKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create protocol registry key: %w", err)
	}
	defer k.Close()

	_ = k.SetStringValue("", "URL:GitSync")
	_ = k.SetStringValue("URL Protocol", "")

	if appKey, _, err := registry.CreateKey(registry.CURRENT_USER, baseKeyPath+`\Application`, registry.SET_VALUE); err == nil {
		_ = appKey.SetStringValue("ApplicationName", "GitSync")
		appKey.Close()
	}

	if iconKey, _, err := registry.CreateKey(registry.CURRENT_USER, baseKeyPath+`\DefaultIcon`, registry.SET_VALUE); err == nil {
		_ = iconKey.SetStringValue("", fmt.Sprintf(`"%s",0`, exePath))
		iconKey.Close()
	}

	cmdKey, _, err := registry.CreateKey(registry.CURRENT_USER, baseKeyPath+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create protocol command key: %w", err)
	}
	defer cmdKey.Close()

	cmdValue := fmt.Sprintf(`"%s" "%%1"`, exePath)
	return cmdKey.SetStringValue("", cmdValue)
}


