package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
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
	clearCmd := SetCurrentCmd(cmd)

	cmd.Stdin = ctx.In
	cmd.Stdout = ctx.Out
	cmd.Stderr = ctx.ErrOut
	if err := cmd.Start(); err != nil {
		clearCmd()
		return func() error { return err }
	}
	return func() error {
		defer clearCmd()
		return cmd.Wait()
	}
}

func (c *Command) Exec(ctx *ExecCtx) error {
	if fn, ok := Builtins[c.Name()]; ok {
		return fn(c, ctx)
	}
	cmd := exec.Command(c.Name(), tokenValues(c.Args())...)
	clearCmd := SetCurrentCmd(cmd)
	defer clearCmd()

	ctx.WireCmd(cmd)
	if ctx.RestoreTerm != nil {
		reenter := ctx.RestoreTerm()
		defer reenter()
	}
	return cmd.Run()
}

type Pipeline struct {
	Stages []*Command
}

func (p *Pipeline) Exec(ctx *ExecCtx) error {
	if ctx.RestoreTerm != nil {
		reenter := ctx.RestoreTerm()
		defer reenter()
	}

	waits := make([]func() error, len(p.Stages))
	var nextIn io.Reader = ctx.In
	for i, stage := range p.Stages {
		localCtx := &ExecCtx{
			In:     nextIn,
			ErrOut: ctx.ErrOut,
		}
		if i == len(p.Stages)-1 {
			localCtx.Out = ctx.Out
			waits[i] = stage.Start(localCtx)
		} else {
			pr, pw, err := os.Pipe()
			if err != nil {
				return fmt.Errorf("unable to create pipe: %v", err)
			}
			localCtx.Out = pw
			nextIn = pr
			wait := stage.Start(localCtx)
			waits[i] = func() error {
				err := wait()
				pw.Close()
				return err
			}
		}
	}

	errs := make([]error, len(waits))
	var wg sync.WaitGroup
	for i, fun := range waits {
		wg.Add(1)
		go func(i int, fun func() error) {
			errs[i] = fun()
			wg.Done()
		}(i, fun)
	}
	wg.Wait()

	return errs[len(waits)-1]
}
