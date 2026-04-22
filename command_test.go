package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCommandExecUsesRunCmd(t *testing.T) {
	cmd := &Command{Tokens: []Token{{TokenWord, "true"}}}

	var called bool
	ctx := &ExecCtx{
		In:     strings.NewReader(""),
		Out:    &strings.Builder{},
		ErrOut: &strings.Builder{},
		RunCmd: func(c *exec.Cmd) error {
			called = true
			return c.Run()
		},
	}

	if err := cmd.Exec(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("Command.Exec did not use RunCmd for external command")
	}
}
