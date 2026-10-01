package main

import (
	"io"
	"os"
	"os/exec"
)

type ExecCtx struct {
	In          io.Reader
	Out         io.Writer
	ErrOut      io.Writer
	SystemIO    *ExecCtx
	RestoreTerm func() func() // restore terminal; returns function to re-enter raw mode

	// Set by Pipeline.Exec for each stage. Pgid points at the pipeline's
	// shared process group id (0 until its first external stage starts and
	// becomes the leader); TTY, if non-nil, is the terminal the leader's
	// group should take as its foreground group.
	Pgid *int
	TTY  *os.File
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
