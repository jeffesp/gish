package main

import (
	"runtime"
	"strings"
	"testing"
	"time"
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

	err := pipeline.Exec(ctx)
	if err != nil {
		t.Fatalf("Pipeline.Exec: %v", err)
	}

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

// requiresSh skips tests that shell out to `sh -c` on platforms without a
// POSIX sh (Windows).
func requiresSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires /bin/sh")
	}
}

func pipelineCtx() *ExecCtx {
	return &ExecCtx{
		In:     strings.NewReader(""),
		Out:    &strings.Builder{},
		ErrOut: &strings.Builder{},
	}
}

func token(s string) Token { return Token{Kind: TokenWord, Value: s} }

// TestPipelineFailedStageIsReported: stage 0 fails (exit 3); stage 1
// receives EOF and exits 0. The failure must still be surfaced — previously
// only the last stage's result was returned, so this pipeline silently
// succeeded.
func TestPipelineFailedStageIsReported(t *testing.T) {
	requiresSh(t)
	pipeline := &Pipeline{Stages: []*Command{
		{Tokens: []Token{token("sh"), token("-c"), token("exit 3")}},
		{Tokens: []Token{token("cat")}},
	}}

	err := pipeline.Exec(pipelineCtx())
	if err == nil {
		t.Fatal("pipeline succeeded, want stage 0's failure")
	}
	if code := exitCode(err); code != 3 {
		t.Errorf("exit code = %d, want 3 (%v)", code, err)
	}
	if !strings.Contains(err.Error(), "stage 0") {
		t.Errorf("error should identify the failed stage: %v", err)
	}
}

// TestPipelineFailedLastStageIsReported: the last stage exits non-zero.
func TestPipelineFailedLastStageIsReported(t *testing.T) {
	requiresSh(t)
	pipeline := &Pipeline{Stages: []*Command{
		{Tokens: []Token{token("echo"), token("hello")}},
		{Tokens: []Token{token("sh"), token("-c"), token("exit 5")}},
	}}

	err := pipeline.Exec(pipelineCtx())
	if err == nil {
		t.Fatal("pipeline succeeded, want stage 1's failure")
	}
	if code := exitCode(err); code != 5 {
		t.Errorf("exit code = %d, want 5 (%v)", code, err)
	}
	if !strings.Contains(err.Error(), "stage 1") {
		t.Errorf("error should identify the failed stage: %v", err)
	}
}

// TestPipelineCommandNotFoundInStage: a missing command in an early stage
// used to be swallowed when a downstream stage exited 0 on EOF.
func TestPipelineCommandNotFoundInStage(t *testing.T) {
	pipeline := &Pipeline{Stages: []*Command{
		{Tokens: []Token{token("definitely-not-a-real-command-xyz")}},
		{Tokens: []Token{token("cat")}},
	}}

	err := pipeline.Exec(pipelineCtx())
	if err == nil {
		t.Fatal("pipeline succeeded, want command-not-found from stage 0")
	}
	if !strings.Contains(err.Error(), "stage 0") {
		t.Errorf("error should identify the failed stage: %v", err)
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-command-xyz") {
		t.Errorf("error should name the missing command: %v", err)
	}
}

// TestPipelineBuiltinStageFailureIsReported: a failing builtin stage (a
// usage error) must surface too, without any external command.
func TestPipelineBuiltinStageFailureIsReported(t *testing.T) {
	pipeline := &Pipeline{Stages: []*Command{
		{Tokens: []Token{token("title")}}, // no args -> usage error
		{Tokens: []Token{token("cat")}},
	}}

	err := pipeline.Exec(pipelineCtx())
	if err == nil {
		t.Fatal("pipeline succeeded, want stage 0's usage error")
	}
	if code := exitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1 (%v)", code, err)
	}
	if !strings.Contains(err.Error(), "stage 0") {
		t.Errorf("error should identify the failed stage: %v", err)
	}
}

// TestPipelineKillsRunningStagesOnFailure: stage 0 blocks; stage 1 fails
// immediately. The pipeline must kill stage 0 instead of waiting out its
// sleep, and the collateral kill must not appear in the reported error.
func TestPipelineKillsRunningStagesOnFailure(t *testing.T) {
	requiresSh(t)
	pipeline := &Pipeline{Stages: []*Command{
		{Tokens: []Token{token("sh"), token("-c"), token("sleep 10")}},
		{Tokens: []Token{token("sh"), token("-c"), token("exit 7")}},
	}}

	start := time.Now()
	err := pipeline.Exec(pipelineCtx())
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("pipeline took %v — the blocked stage was not killed", elapsed)
	}
	if err == nil {
		t.Fatal("pipeline succeeded, want stage 1's failure")
	}
	if code := exitCode(err); code != 7 {
		t.Errorf("exit code = %d, want 7 (%v)", code, err)
	}
	if !strings.Contains(err.Error(), "stage 1") {
		t.Errorf("error should identify the failed stage: %v", err)
	}
	if strings.Contains(err.Error(), "stage 0") {
		t.Errorf("killed stage 0 should not be reported: %v", err)
	}
}
