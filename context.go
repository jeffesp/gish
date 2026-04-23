package main

import (
	"io"
	"os/exec"
)

type ExecCtx struct {
	In          io.Reader
	Out         io.Writer
	ErrOut      io.Writer
	RunCmd      func(*exec.Cmd) error // terminal restore/raw mode callback
	RestoreTerm func() func()         // restore terminal; returns function to re-enter raw mode
}
