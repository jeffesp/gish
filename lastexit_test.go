package main

import (
	"os"
	"strings"
	"testing"
)

func TestRecordExitCode(t *testing.T) {
	recordExitCode(3)
	if lastExitCode != 3 {
		t.Errorf("lastExitCode = %d, want 3", lastExitCode)
	}
	if v := os.Getenv("GISH_LASTEXIT"); v != "3" {
		t.Errorf("GISH_LASTEXIT = %q, want %q", v, "3")
	}

	recordExitCode(0)
	if lastExitCode != 0 {
		t.Errorf("lastExitCode = %d, want 0", lastExitCode)
	}
	if v := os.Getenv("GISH_LASTEXIT"); v != "0" {
		t.Errorf("GISH_LASTEXIT = %q, want %q", v, "0")
	}
}

func TestExpandDollarQuestion(t *testing.T) {
	recordExitCode(42)

	cases := []struct {
		name  string
		input Token
		want  string
	}{
		{"bare", Token{TokenWord, "$?"}, "42"},
		{"braced", Token{TokenWord, "${?}"}, "42"},
		{"embedded", Token{TokenWord, "code-$?"}, "code-42"},
		{"double quoted", Token{TokenDoubleQuoted, "$?"}, "42"},
		{"single quoted does not expand", Token{TokenSingleQuoted, "$?"}, "$?"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandToken(c.input)
			if got.Value != c.want {
				t.Errorf("expandToken(%q): got %q want %q", c.input.Value, got.Value, c.want)
			}
		})
	}
}

func TestExecLineUpdatesDollarQuestion(t *testing.T) {
	out := &strings.Builder{}
	ctx := &ExecCtx{
		In:     strings.NewReader(""),
		Out:    out,
		ErrOut: &strings.Builder{},
	}

	// failing command -> $? becomes 1
	execLine("false", ctx)
	out.Reset()
	execLine("echo $?", ctx)
	if got := out.String(); got != "1\n" {
		t.Errorf(`after "false", echo $? printed %q, want "1\n"`, got)
	}

	// failing command -> $? stays 1
	execLine("true", ctx)
	out.Reset()
	execLine("echo ${?}", ctx)
	if got := out.String(); got != "0\n" {
		t.Errorf(`after "true", echo ${?} printed %q, want "0\n"`, got)
	}

	// command not found -> exit code 1
	out.Reset()
	execLine("definitely-not-a-real-cmd-12345", ctx)
	execLine("echo $?", ctx)
	if got := out.String(); got != "1\n" {
		t.Errorf("after missing command, echo $? printed %q, want \"1\\n\"", got)
	}
}
