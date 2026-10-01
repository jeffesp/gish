//go:build windows

package main

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on Windows: POSIX process groups don't
// exist there, and a full-tree kill would require setting up a Job Object.
func setProcessGroup(cmd *exec.Cmd, pgid int, tty *os.File) {}

// isTerminal is always false on Windows: there is no foreground process
// group to hand over.
func isTerminal(f *os.File) bool { return false }

// reclaimTerminal is a no-op on Windows (see isTerminal).
func reclaimTerminal(tty *os.File) {}

// killProcessGroup kills only the process cmd started; descendants it
// forked are not tracked without a Job Object (see setProcessGroup).
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Kill() //nolint:errcheck
	}
}
