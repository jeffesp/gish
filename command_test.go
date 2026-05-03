package main

import (
	"strings"
	"testing"
)

func TestCommandExecCallsWireCmd(t *testing.T) {
	cmd := &Command{Tokens: []Token{{TokenWord, "/bin/echo"}, {TokenWord, "hello"}}}

	systemOut := &strings.Builder{}
	systemCtx := &ExecCtx{
		In:     strings.NewReader(""),
		Out:    systemOut,
		ErrOut: &strings.Builder{},
	}

	ctxOut := &strings.Builder{}
	ctx := &ExecCtx{
		In:       strings.NewReader(""),
		Out:      ctxOut,
		ErrOut:   &strings.Builder{},
		SystemIO: systemCtx,
	}

	if err := cmd.Exec(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if systemOut.String() != "hello\n" {
		t.Errorf("expected output on SystemIO.Out, got %q", systemOut.String())
	}
	if ctxOut.String() != "" {
		t.Errorf("expected ctx.Out to be unused, got %q", ctxOut.String())
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
