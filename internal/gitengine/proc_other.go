//go:build !windows

package gitengine

import (
	"os"
	"os/exec"
)

// configureHiddenCmd configures non-interactive environment variables for background git execution.
func configureHiddenCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if len(cmd.Env) == 0 {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
	)
}

