//go:build unix

package api

import (
	"errors"
	"os/exec"
	"syscall"
)

// isolateProcessGroup puts the command in its own process group and makes the context
// cancellation kill that whole group, so a timed out check cannot leave children behind.
func isolateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
}

// killProcessGroup kills every process of the command's group.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
