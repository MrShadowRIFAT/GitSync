//go:build windows

package gitengine

import (
	"os"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// configureHiddenCmd configures an exec.Cmd on Windows so that git.exe and any child
// helpers run completely silently in the background without flashing a CMD window
// or triggering interactive terminal prompts.
func configureHiddenCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	if len(cmd.Env) == 0 {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
	)
}

