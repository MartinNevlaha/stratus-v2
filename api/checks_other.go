//go:build !unix

package api

import "os/exec"

// isolateProcessGroup is a no-op where process groups are unavailable: only the
// direct child is killed on a timeout.
func isolateProcessGroup(*exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
