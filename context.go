package main

import (
	"io"
	"os/exec"
)

type ExecCtx struct {
	In          io.Reader
	Out         io.Writer
	ErrOut      io.Writer
	SystemIO    *ExecCtx
	RestoreTerm func() func() // restore terminal; returns function to re-enter raw mode
	JobMgr      *JobManager
	TrackCmd    func(*exec.Cmd) func()
	// PrepareProc, if set, is called just before a child is started and returns
	// a hook to run just after.  Jobs use it to put every stage of a pipeline
	// into one process group with the job's PTY as its controlling terminal.
	PrepareProc func(*exec.Cmd) func()
	// Pushback returns input the shell read but did not consume — the bytes
	// typed after a Ctrl+Z — so the line editor picks them up next.
	Pushback func([]byte)
}

// Interactive reports whether the shell is driving a real terminal.  Only then
// is it right to give foreground commands their own PTY: a scripted shell
// should hand children the real stdin rather than fabricate a terminal for them.
func (ctx *ExecCtx) Interactive() bool {
	return ctx.RestoreTerm != nil
}

func (ctx *ExecCtx) WireCmd(cmd *exec.Cmd) {
	src := ctx
	if ctx.SystemIO != nil {
		src = ctx.SystemIO
	}
	cmd.Stdin = src.In
	cmd.Stdout = src.Out
	cmd.Stderr = src.ErrOut
}
