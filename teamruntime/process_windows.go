//go:build windows

package teamruntime

import "os/exec"

func prepareProcess(cmd *exec.Cmd) {}
func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
func killProcess(cmd *exec.Cmd) { terminateProcess(cmd) }
