//go:build windows

package updater

import (
	"os/exec"
	"syscall"
)

func configureBackgroundCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

