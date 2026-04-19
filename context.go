package main

import (
	"fmt"
	"io"
	"os/exec"
)

type ExecCtx struct {
	In          io.Reader
	Out         io.Writer
	ErrOut      io.Writer
	RunCmd      func(*exec.Cmd) error // terminal restore/raw mode callback
	RestoreTerm func()                // restore terminal before process replacement (exec)
}

type Executable interface {
	Exec(ctx *ExecCtx) error
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

func (c *Command) Exec(ctx *ExecCtx) error {
	if fn, ok := Builtins[c.Name()]; ok {
		return fn(c, ctx)
	}
	cmd := exec.Command(c.Name(), tokenValues(c.Args())...)
	cmd.Stdin = ctx.In
	cmd.Stdout = ctx.Out
	cmd.Stderr = ctx.ErrOut
	SetCurrentCmd(cmd)
	err := ctx.RunCmd(cmd)
	ClearCurrentCmd()
	return err
}

type Pipeline struct {
	Stages []*Command
}

func (p *Pipeline) Exec(ctx *ExecCtx) error {
	return fmt.Errorf("pipelines not yet implemented")
}
