package main

import (
	"fmt"
	"os/exec"
)

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

func (c *Command) Start(ctx *ExecCtx) (wait func() error) {
	if fn, ok := Builtins[c.Name()]; ok {
		done := make(chan error, 1)
		go func() { done <- fn(c, ctx) }()
		return func() error { return <-done }
	}
	cmd := exec.Command(c.Name(), tokenValues(c.Args())...)
	cmd.Stdin = ctx.In
	cmd.Stdout = ctx.Out
	cmd.Stderr = ctx.ErrOut
	cmd.Start()
	return cmd.Wait
}

func (c *Command) Exec(ctx *ExecCtx) error {
	if fn, ok := Builtins[c.Name()]; ok {
		return fn(c, ctx)
	}
	cmd := exec.Command(c.Name(), tokenValues(c.Args())...)
	if ctx.RunCmd != nil {
		return ctx.RunCmd(cmd)
	}
	cmd.Stdin = ctx.In
	cmd.Stdout = ctx.Out
	cmd.Stderr = ctx.ErrOut
	return cmd.Run()
}

type Pipeline struct {
	Stages []*Command
}

func (p *Pipeline) Exec(ctx *ExecCtx) error {
	// todo: create len(Stages)-1 os.Pipe()s, wire output of prev to input of next,
	// somehow make them call Start for the commands, then do something to handle
	// everything finishing (or erroring)

	return fmt.Errorf("pipelines not yet implemented")
}
