package main

import (
	"bytes"
	"io"
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

func testCtx(out io.Writer) *ExecCtx {
	return &ExecCtx{In: strings.NewReader(""), Out: out, ErrOut: io.Discard}
}

func testCmd(args []Token) *Command {
	tokens := append([]Token{{TokenWord, "test"}}, args...)
	return &Command{Tokens: tokens, Line: ""}
}

func TestBuiltinEcho(t *testing.T) {
	cases := []struct {
		args []Token
		want string
	}{
		{words("hello"), "hello\n"},
		{words("hello", "world"), "hello world\n"},
		// no args
		{[]Token{}, "\n"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := builtinEcho(testCmd(c.args), testCtx(&buf)); err != nil {
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
	if err := builtinSet(testCmd(words("GISH_SET_TEST", "hello")), testCtx(&buf)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("GISH_SET_TEST"); got != "hello" {
		t.Errorf("got %q want %q", got, "hello")
	}
}

func TestBuiltinSetErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := builtinSet(testCmd(words()), testCtx(&buf)); err == nil {
		t.Error("expected error for zero args")
	}
	if err := builtinSet(testCmd(words("KEY")), testCtx(&buf)); err == nil {
		t.Error("expected error for one arg")
	}
	if err := builtinSet(testCmd(words("KEY", "VAL", "EXTRA")), testCtx(&buf)); err == nil {
		t.Error("expected error for three args")
	}
}

func TestBuiltinUnset(t *testing.T) {
	os.Setenv("GISH_UNSET_TEST", "value")
	var buf bytes.Buffer
	if err := builtinUnset(testCmd(words("GISH_UNSET_TEST")), testCtx(&buf)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("GISH_UNSET_TEST"); got != "" {
		t.Errorf("var still set: %q", got)
	}
}

func TestBuiltinUnsetErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := builtinUnset(testCmd(words()), testCtx(&buf)); err == nil {
		t.Error("expected error for zero args")
	}
	if err := builtinUnset(testCmd(words("A", "B")), testCtx(&buf)); err == nil {
		t.Error("expected error for two args")
	}
}

func TestBuiltinEnv(t *testing.T) {
	os.Setenv("GISH_ENV_TEST", "present")
	defer os.Unsetenv("GISH_ENV_TEST")

	var buf bytes.Buffer
	if err := builtinEnv(testCmd(words()), testCtx(&buf)); err != nil {
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

	if err := builtinCd(testCmd(words(tmp)), testCtx(&buf)); err != nil {
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
	if err := builtinCd(testCmd(words()), testCtx(&buf)); err != nil {
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
	if err := builtinCd(testCmd(words("a", "b")), testCtx(&buf)); err == nil {
		t.Error("expected error for two args")
	}
	if err := builtinCd(testCmd(words("/nonexistent/path/gish")), testCtx(&buf)); err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestExecLineExpansion(t *testing.T) {
	os.Setenv("GISH_EXEC_TEST", "expanded")
	defer os.Unsetenv("GISH_EXEC_TEST")

	var buf bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &buf, ErrOut: io.Discard}
	execLine("echo $GISH_EXEC_TEST", ctx)
	if got := buf.String(); got != "expanded\n" {
		t.Errorf("execLine echo $VAR: got %q want %q", got, "expanded\n")
	}

	// single-quoted should not expand
	buf.Reset()
	execLine("echo '$GISH_EXEC_TEST'", ctx)
	if got := buf.String(); got != "$GISH_EXEC_TEST\n" {
		t.Errorf("execLine echo single-quoted: got %q want %q", got, "$GISH_EXEC_TEST\n")
	}
}

func TestExecLineCdExpansion(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	tmp := t.TempDir()
	os.Setenv("GISH_CD_TEST", tmp)
	defer os.Unsetenv("GISH_CD_TEST")

	var buf bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &buf, ErrOut: io.Discard}
	execLine("cd $GISH_CD_TEST", ctx)

	got, _ := os.Getwd()
	gotR, _ := filepath.EvalSymlinks(got)
	tmpR, _ := filepath.EvalSymlinks(tmp)
	if gotR != tmpR {
		t.Errorf("cd $GISH_CD_TEST: cwd=%q want=%q", gotR, tmpR)
	}
}

func TestBuiltinClear(t *testing.T) {
	var buf bytes.Buffer
	if err := builtinClear(testCmd(words()), testCtx(&buf)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "\033[") {
		t.Errorf("clear output missing escape sequence: %q", buf.String())
	}
}
