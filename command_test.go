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

func TestPipelineRunsMultipleCommands(t *testing.T) {
	pipeline := &Pipeline{Stages: []*Command{
		{Tokens: []Token{{TokenWord, "echo"}, {TokenWord, "hello"}}},
		{Tokens: []Token{{TokenWord, "cat"}}},
	}}

	var restoreCalled, termRawCalled bool
	output := &strings.Builder{}
	ctx := &ExecCtx{
		In:     strings.NewReader(""),
		Out:    output,
		ErrOut: &strings.Builder{},
		RunCmd: func(c *exec.Cmd) error {
			return c.Run()
		},
		RestoreTerm: func() func() {
			restoreCalled = true
			return func() { termRawCalled = true }
		},
	}

	pipeline.Exec(ctx)

	if output.String() != "hello\n" {
		t.Errorf("Pipeline did not exec commands. Expected: %s, got: %s", "hello", output.String())
	}

	if !restoreCalled {
		t.Error("Pipeline.Exec did not call RestoreTerm")
	}

	if !termRawCalled {
		t.Error("Pipeline.Exec did not call RestoreTerm reset return func")
	}
}
