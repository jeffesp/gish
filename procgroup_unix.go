//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setNewProcessGroup makes cmd the leader of a new process group, so
// killProcessGroup can terminate it together with any children it forks
// (e.g. a shell running "sh -c '...'") that would otherwise survive a
// kill of the leader alone and keep inherited pipe fds open.
func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the process group led by cmd's process. Setpgid
// (set by setNewProcessGroup before Start) makes the group id equal the
// leader's pid, so the negative pid targets the whole group.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Failure is fine: the group may already be gone; either way Wait()
	// reaps the leader.
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck
}
