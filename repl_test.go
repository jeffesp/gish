package main

import (
	"testing"
)

func mustTokenize(t *testing.T, s string) []Token {
	t.Helper()
	tokens, err := tokenize(s)
	if err != nil {
		t.Fatalf("tokenize(%q): %v", s, err)
	}
	return tokens
}

func TestParseTokens(t *testing.T) {
	t.Run("single command returns Command", func(t *testing.T) {
		tokens := mustTokenize(t, "echo hello world")
		exe, _, err := parseTokens(tokens, "echo hello world")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cmd, ok := exe.(*Command)
		if !ok {
			t.Fatalf("expected *Command, got %T", exe)
		}
		if cmd.Name() != "echo" {
			t.Errorf("Name() = %q, want %q", cmd.Name(), "echo")
		}
		if cmd.Line != "echo hello world" {
			t.Errorf("Line = %q, want %q", cmd.Line, "echo hello world")
		}
	})

	t.Run("two-stage pipeline returns Pipeline", func(t *testing.T) {
		tokens := mustTokenize(t, "cat file.txt | grep foo")
		exe, _, err := parseTokens(tokens, "cat file.txt | grep foo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p, ok := exe.(*Pipeline)
		if !ok {
			t.Fatalf("expected *Pipeline, got %T", exe)
		}
		if len(p.Stages) != 2 {
			t.Fatalf("len(Stages) = %d, want 2", len(p.Stages))
		}
		if p.Stages[0].Name() != "cat" {
			t.Errorf("stage 0 name = %q, want %q", p.Stages[0].Name(), "cat")
		}
		if p.Stages[1].Name() != "grep" {
			t.Errorf("stage 1 name = %q, want %q", p.Stages[1].Name(), "grep")
		}
	})

	t.Run("three-stage pipeline", func(t *testing.T) {
		tokens := mustTokenize(t, "cat file.txt | grep foo | sort")
		exe, _, err := parseTokens(tokens, "cat file.txt | grep foo | sort")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p, ok := exe.(*Pipeline)
		if !ok {
			t.Fatalf("expected *Pipeline, got %T", exe)
		}
		if len(p.Stages) != 3 {
			t.Fatalf("len(Stages) = %d, want 3", len(p.Stages))
		}
		wantNames := []string{"cat", "grep", "sort"}
		for i, want := range wantNames {
			if got := p.Stages[i].Name(); got != want {
				t.Errorf("stage %d name = %q, want %q", i, got, want)
			}
		}
	})

	t.Run("pipeline stage args", func(t *testing.T) {
		tokens := mustTokenize(t, "grep foo bar | sort -r")
		exe, _, err := parseTokens(tokens, "grep foo bar | sort -r")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		p := exe.(*Pipeline)
		if got := tokenValues(p.Stages[0].Args()); len(got) != 2 || got[0] != "foo" || got[1] != "bar" {
			t.Errorf("stage 0 args = %v, want [foo bar]", got)
		}
		if got := tokenValues(p.Stages[1].Args()); len(got) != 1 || got[0] != "-r" {
			t.Errorf("stage 1 args = %v, want [-r]", got)
		}
	})

	t.Run("recognize background job", func(t *testing.T) {
		tokens := mustTokenize(t, "sleep 100 &")
		_, isBackground, err := parseTokens(tokens, "sleep 100 &")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !isBackground {
			t.Error("command not marked as background")
		}
	})

	t.Run("leading pipe is error", func(t *testing.T) {
		tokens := mustTokenize(t, "| grep foo")
		_, _, err := parseTokens(tokens, "| grep foo")
		if err == nil {
			t.Error("expected error for leading pipe, got nil")
		}
	})

	t.Run("trailing pipe is error", func(t *testing.T) {
		tokens := mustTokenize(t, "grep foo |")
		_, _, err := parseTokens(tokens, "grep foo |")
		if err == nil {
			t.Error("expected error for trailing pipe, got nil")
		}
	})

	t.Run("consecutive pipes are error", func(t *testing.T) {
		tokens := mustTokenize(t, "grep foo | | sort")
		_, _, err := parseTokens(tokens, "grep foo | | sort")
		if err == nil {
			t.Error("expected error for consecutive pipes, got nil")
		}
	})

	t.Run("merge pipe is recognized as distinct from normal pipe", func(t *testing.T) {
		tokens := mustTokenize(t, "grep foo |& sort | uniq")
		exe, _, err := parseTokens(tokens, "grep foo |& sort | uniq")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		p := exe.(*Pipeline)
		if len(p.MergeErr) != 2 {
			t.Error("did not find the right number of pipes")
		}
		if !p.MergeErr[0] {
			t.Errorf("did not set that first pipe should merge stderr into output")
		}
		if p.MergeErr[1] {
			t.Errorf("did not set that first pipe should NOT merge stderr into output")
		}
	})
}
