package main

import (
	"io"
	"os/exec"
)

type ExecCtx struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
	RunCmd func(*exec.Cmd) error // terminal restore/raw mode callback
}

type Command struct {
	Tokens []Token
	Line   string
}

func (cmd *Command) Args() []Token {
	if len(cmd.Tokens) < 2 {
		return nil
	}
	return cmd.Tokens[1:]
}

func (cmd *Command) Name() string {
	if len(cmd.Tokens) == 0 {
		return ""
	}
	return cmd.Tokens[0].Value
}
