package main

import (
	"os"
	"runtime"
	"runtime/debug"
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

// TestPipelineClosesPipeFds verifies the parent's copies of the pipe ends
// are closed when the pipeline's stages finish, so a long session doesn't
// accumulate one fd per pipeline stage. The check is fd-number reuse: the
// kernel allocates the lowest free fd, so if the pipelines leaked pipe ends
// (and GC is suspended below, so the runtime's *os.File finalizer can't
// reclaim them), the next os.Pipe would be pushed to higher numbers.
func TestPipelineClosesPipeFds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fd-number allocation is not a reliable leak signal on Windows")
	}

	// Highest fd number a fresh pipe is handed; both ends are closed again.
	newPipeFd := func() int {
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe: %v", err)
		}
		defer pr.Close()
		defer pw.Close()
		if pr.Fd() > pw.Fd() {
			return int(pr.Fd())
		}
		return int(pw.Fd())
	}

	previousGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGCPercent)

	scenarios := map[string][]*Command{
		"success": {
			{Tokens: []Token{token("echo"), token("hello")}},
			{Tokens: []Token{token("cat")}},
		},
		// A stage that fails before ever reading stdin (command not
		// found) must release its pipe end too.
		"stage fails before reading stdin": {
			{Tokens: []Token{token("definitely-not-a-real-command-xyz")}},
			{Tokens: []Token{token("cat")}},
		},
	}

	for name, template := range scenarios {
		t.Run(name, func(t *testing.T) {
			baseline := newPipeFd()
			for i := 0; i < 10; i++ {
				stages := make([]*Command, len(template))
				for j, s := range template {
					tokens := make([]Token, len(s.Tokens))
					copy(tokens, s.Tokens)
					stages[j] = &Command{Tokens: tokens}
				}
				(&Pipeline{Stages: stages}).Exec(pipelineCtx())
			}
			if got := newPipeFd(); got > baseline+2 {
				t.Errorf("fd numbers jumped from %d to %d — pipe ends leaked", baseline, got)
			}
		})
	}
}
