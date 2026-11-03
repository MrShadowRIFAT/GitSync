//go:build windows

package config

import (
	"golang.org/x/sys/windows/registry"
)

// getPlatformMachineID retrieves the unique Windows MachineGuid from the registry.
func getPlatformMachineID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return guid
}
