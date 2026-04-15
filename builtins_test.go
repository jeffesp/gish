package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func words(vals ...string) []Token {
	tokens := make([]Token, len(vals))
	for i, v := range vals {
		tokens[i] = Token{Kind: TokenWord, Value: v}
	}
	return tokens
}

func TestBuiltinEcho(t *testing.T) {
	os.Setenv("GISH_TEST_VAR", "world")
	defer os.Unsetenv("GISH_TEST_VAR")

	cases := []struct {
		args []Token
		want string
	}{
		{words("hello"), "hello\n"},
		{words("hello", "world"), "hello world\n"},
		{words("$GISH_TEST_VAR"), "world\n"},
		// single-quoted: no expansion
		{[]Token{{TokenSingleQuoted, "$GISH_TEST_VAR"}}, "$GISH_TEST_VAR\n"},
		// double-quoted: expansion (same as bare word for now)
		{[]Token{{TokenDoubleQuoted, "$GISH_TEST_VAR"}}, "world\n"},
		// no args
		{[]Token{}, "\n"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := builtinEcho(c.args, &buf); err != nil {
			t.Errorf("echo(%v): unexpected error: %v", c.args, err)
		}
		if got := buf.String(); got != c.want {
			t.Errorf("echo(%v): got %q want %q", c.args, got, c.want)
		}
	}
}

func TestBuiltinSet(t *testing.T) {
	defer os.Unsetenv("GISH_SET_TEST")

	var buf bytes.Buffer
	if err := builtinSet(words("GISH_SET_TEST", "hello"), &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("GISH_SET_TEST"); got != "hello" {
		t.Errorf("got %q want %q", got, "hello")
	}
}

func TestBuiltinSetErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := builtinSet(words(), &buf); err == nil {
		t.Error("expected error for zero args")
	}
	if err := builtinSet(words("KEY"), &buf); err == nil {
		t.Error("expected error for one arg")
	}
	if err := builtinSet(words("KEY", "VAL", "EXTRA"), &buf); err == nil {
		t.Error("expected error for three args")
	}
}

func TestBuiltinUnset(t *testing.T) {
	os.Setenv("GISH_UNSET_TEST", "value")
	var buf bytes.Buffer
	if err := builtinUnset(words("GISH_UNSET_TEST"), &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("GISH_UNSET_TEST"); got != "" {
		t.Errorf("var still set: %q", got)
	}
}

func TestBuiltinUnsetErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := builtinUnset(words(), &buf); err == nil {
		t.Error("expected error for zero args")
	}
	if err := builtinUnset(words("A", "B"), &buf); err == nil {
		t.Error("expected error for two args")
	}
}

func TestBuiltinEnv(t *testing.T) {
	os.Setenv("GISH_ENV_TEST", "present")
	defer os.Unsetenv("GISH_ENV_TEST")

	var buf bytes.Buffer
	if err := builtinEnv(words(), &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "GISH_ENV_TEST=present") {
		t.Errorf("env output missing GISH_ENV_TEST=present")
	}
}

func TestBuiltinCd(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	tmp := t.TempDir()
	var buf bytes.Buffer

	if err := builtinCd(words(tmp), &buf); err != nil {
		t.Fatalf("cd to tmp: unexpected error: %v", err)
	}
	got, _ := os.Getwd()
	// resolve symlinks so comparison works on macOS /var -> /private/var
	gotR, _ := filepath.EvalSymlinks(got)
	tmpR, _ := filepath.EvalSymlinks(tmp)
	if gotR != tmpR {
		t.Errorf("after cd: cwd=%q want=%q", gotR, tmpR)
	}
}

func TestBuiltinCdHome(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	home := os.Getenv("HOME")
	var buf bytes.Buffer
	if err := builtinCd(words(), &buf); err != nil {
		t.Fatalf("cd home: unexpected error: %v", err)
	}
	got, _ := os.Getwd()
	gotR, _ := filepath.EvalSymlinks(got)
	homeR, _ := filepath.EvalSymlinks(home)
	if gotR != homeR {
		t.Errorf("after cd home: cwd=%q want=%q", gotR, homeR)
	}
}

func TestBuiltinCdErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := builtinCd(words("a", "b"), &buf); err == nil {
		t.Error("expected error for two args")
	}
	if err := builtinCd(words("/nonexistent/path/gish"), &buf); err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestBuiltinClear(t *testing.T) {
	var buf bytes.Buffer
	if err := builtinClear(words(), &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "\033[") {
		t.Errorf("clear output missing escape sequence: %q", buf.String())
	}
}
