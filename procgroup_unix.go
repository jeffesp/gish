//go:build !windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
	xterm "golang.org/x/term"
)

// setProcessGroup puts cmd in the process group pgid, or makes it the
// leader of a new group when pgid is 0. Pipeline stages share one group so
// the whole pipeline can be killed together (a stage like "sh -c '...'" may
// fork children that would otherwise survive a kill of the leader alone and
// keep inherited pipe fds open) and handed the terminal as a unit.
//
// A non-nil tty additionally makes the group the terminal's foreground
// group; the child does this itself before exec, so it can't race with the
// program's first read of /dev/tty. Only the group's leader should pass one.
func setProcessGroup(cmd *exec.Cmd, pgid int, tty *os.File) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid}
	if tty != nil {
		cmd.SysProcAttr.Foreground = true
		cmd.SysProcAttr.Ctty = int(tty.Fd())
	}
}

// isTerminal reports whether f is a terminal.
func isTerminal(f *os.File) bool {
	return f != nil && xterm.IsTerminal(int(f.Fd()))
}

// reclaimTerminal makes gish's own process group the foreground group of
// tty again, once a pipeline's group is finished with it. gish is a
// background group at that point, so tcsetpgrp would stop it with SIGTTOU;
// the signal is ignored only for the duration of the call because an
// ignored disposition is inherited by children across exec.
func reclaimTerminal(tty *os.File) {
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	unix.IoctlSetPointerInt(int(tty.Fd()), unix.TIOCSPGRP, syscall.Getpgrp()) //nolint:errcheck
}

// killProcessGroup kills the process group led by cmd's process. Setpgid
// (set by setProcessGroup before Start) makes the group id equal the
// leader's pid, so the negative pid targets the whole group.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Failure is fine: the group may already be gone; either way Wait()
	// reaps the leader.
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck
}
