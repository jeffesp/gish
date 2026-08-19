package main

import (
	"strings"
	"testing"
)

// TestExecEscapedSpace runs a command through the full
// tokenize -> expand -> parse -> exec flow and verifies the escaped space
// reaches the child as a literal argument character.
func TestExecEscapedSpace(t *testing.T) {
	line := `echo a\ b`
	tokens, err := tokenize(line)
	if err != nil {
		t.Fatalf("tokenize: %v", err)
	}
	tokens = expandVars(tokens)
	exe, _, err := parseTokens(tokens, line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cmd := exe.(*Command)

	out := &strings.Builder{}
	ctx := &ExecCtx{
		In:     strings.NewReader(""),
		Out:    out,
		ErrOut: &strings.Builder{},
	}
	if err := cmd.Exec(ctx); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if got := out.String(); got != "a b\n" {
		t.Errorf("got %q, want %q", got, "a b\n")
	}
}

func TestCommandExecCallsWireCmd(t *testing.T) {
	cmd := &Command{Tokens: []Token{{Kind: TokenWord, Value: "/bin/echo"}, {Kind: TokenWord, Value: "hello"}}}

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
		{Tokens: []Token{{Kind: TokenWord, Value: "echo"}, {Kind: TokenWord, Value: "hello"}}},
		{Tokens: []Token{{Kind: TokenWord, Value: "cat"}}},
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
