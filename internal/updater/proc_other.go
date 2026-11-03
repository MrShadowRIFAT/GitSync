//go:build !windows

package updater

import "os/exec"

func configureBackgroundCmd(_ *exec.Cmd) {}

