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
