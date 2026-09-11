//go:build windows

package main

import "os/exec"

// setNewProcessGroup is a no-op on Windows: POSIX process groups don't
// exist there, and a full-tree kill would require setting up a Job Object.
func setNewProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup kills only the process cmd started; descendants it
// forked are not tracked without a Job Object (see setNewProcessGroup).
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Kill() //nolint:errcheck
	}
}
