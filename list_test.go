package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTokenizeListOperators(t *testing.T) {
	cases := []struct {
		in   string
		want []TokenKind
	}{
		{"a ; b", []TokenKind{TokenWord, TokenSemi, TokenWord}},
		{"a;b", []TokenKind{TokenWord, TokenSemi, TokenWord}},
		{"a && b", []TokenKind{TokenWord, TokenAnd, TokenWord}},
		{"a&&b", []TokenKind{TokenWord, TokenAnd, TokenWord}},
		{"a || b", []TokenKind{TokenWord, TokenOr, TokenWord}},
		{"a | b", []TokenKind{TokenWord, TokenPipe, TokenWord}},
		{"a |& b", []TokenKind{TokenWord, TokenMergePipe, TokenWord}},
		{"a &", []TokenKind{TokenWord, TokenBackground}},
		{`a ";" b`, []TokenKind{TokenWord, TokenDoubleQuoted, TokenWord}},
		{`a '&&' b`, []TokenKind{TokenWord, TokenSingleQuoted, TokenWord}},
		{`a \; b`, []TokenKind{TokenWord, TokenWord, TokenWord}},
		{`a \&\& b`, []TokenKind{TokenWord, TokenWord, TokenWord}},
	}
	for _, c := range cases {
		got := mustTokenize(t, c.in)
		if len(got) != len(c.want) {
			t.Errorf("tokenize(%q): got %d tokens %v, want %d", c.in, len(got), got, len(c.want))
			continue
		}
		for i, k := range c.want {
			if got[i].Kind != k {
				t.Errorf("tokenize(%q)[%d].Kind = %v, want %v", c.in, i, got[i].Kind, k)
			}
		}
	}
}

func TestSplitList(t *testing.T) {
	items, err := splitList(mustTokenize(t, "a b && c || d ; e | f"))
	if err != nil {
		t.Fatal(err)
	}
	wantOps := []TokenKind{TokenSemi, TokenAnd, TokenOr, TokenSemi}
	wantLens := []int{2, 1, 1, 3}
	if len(items) != len(wantOps) {
		t.Fatalf("got %d items, want %d", len(items), len(wantOps))
	}
	for i := range items {
		if items[i].Op != wantOps[i] || len(items[i].Tokens) != wantLens[i] {
			t.Errorf("item %d = op %v len %d, want op %v len %d", i, items[i].Op, len(items[i].Tokens), wantOps[i], wantLens[i])
		}
	}

	if items, err := splitList(mustTokenize(t, "a ;")); err != nil || len(items) != 1 {
		t.Errorf("trailing ';': items=%d err=%v, want 1 item and no error", len(items), err)
	}
	if items, err := splitList(nil); err != nil || len(items) != 0 {
		t.Errorf("empty: items=%d err=%v", len(items), err)
	}
	for _, bad := range []string{"; a", "a ;; b", "a &&", "&& a", "a && && b", "a || ;", "||"} {
		if _, err := splitList(mustTokenize(t, bad)); err == nil {
			t.Errorf("splitList(%q): expected syntax error", bad)
		}
	}
}

func runLine(t *testing.T, line string) (out, errOut string) {
	t.Helper()
	var o, e syncBuffer
	execLine(line, &ExecCtx{In: strings.NewReader(""), Out: &o, ErrOut: &e})
	return o.String(), e.String()
}

// syncBuffer is a bytes.Buffer safe for the concurrent writers that exec
// starts when it copies several stages' output into one non-file writer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestExecLineLists(t *testing.T) {
	t.Setenv("GISH_HISTORY_FILE", t.TempDir()+"/h")
	historyFile = t.TempDir() + "/h"
	cases := []struct {
		line string
		want string
	}{
		{"echo a; echo b", "a\nb\n"},
		{"echo a;echo b;", "a\nb\n"},
		{"true && echo yes", "yes\n"},
		{"false && echo no", ""},
		{"false || echo fallback", "fallback\n"},
		{"true || echo no", ""},
		{"false && echo no || echo rescue", "rescue\n"},
		{"true && echo a || echo b", "a\n"},
		{"false; echo $?", "1\n"},
		{"false || echo $?", "1\n"},
		{"false && true; echo $?", "1\n"},
		{"echo a | cat && echo b", "a\nb\n"},
		{"echo ';' '&&' '||'", "; && ||\n"},
		{"echo a ; false ; echo b", "a\nb\n"},
	}
	for _, c := range cases {
		out, _ := runLine(t, c.line)
		if out != c.want {
			t.Errorf("execLine(%q) output = %q, want %q", c.line, out, c.want)
		}
	}
}

func TestExecLineListSequencesState(t *testing.T) {
	historyFile = t.TempDir() + "/h"
	t.Chdir(t.TempDir()) // restores the working directory when the test ends
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	// Single quotes keep Windows backslashes from being read as escapes.
	_, errOut := runLine(t, "cd '"+dir+"' && echo ok")
	got, _ := os.Getwd()
	if got, _ = filepath.EvalSymlinks(got); got != dir {
		t.Errorf("cwd after cd && ... = %q (stderr %q), want %q", got, errOut, dir)
	}
}

func TestExecLineListSyntaxErrorRunsNothing(t *testing.T) {
	historyFile = t.TempDir() + "/h"
	out, errOut := runLine(t, "echo a && && echo b")
	if out != "" || !strings.Contains(errOut, "syntax error") {
		t.Errorf("out=%q err=%q, want no output and a syntax error", out, errOut)
	}
}

func TestExecLineBackgroundRefused(t *testing.T) {
	historyFile = filepath.Join(t.TempDir(), "h")
	out, errOut := runLine(t, "echo hi &")
	if out != "" || !strings.Contains(errOut, "background jobs") {
		t.Errorf("out=%q err=%q, want no output and a background-jobs error", out, errOut)
	}
}

func TestAliasBodyWithListOperators(t *testing.T) {
	historyFile = filepath.Join(t.TempDir(), "h")
	setAlias("gishtwice", "echo one && echo two")
	setAlias("gishq", "echo q")
	defer unsetAlias("gishtwice")
	defer unsetAlias("gishq")

	if out, _ := runLine(t, "gishtwice"); out != "one\ntwo\n" {
		t.Errorf("alias with &&: %q", out)
	}
	// Aliases expand at the start of every item, not only after a pipe.
	if out, _ := runLine(t, "true && gishq; gishq"); out != "q\nq\n" {
		t.Errorf("alias after && and ;: %q", out)
	}
	if out, _ := runLine(t, "echo a |& gishq"); out != "q\n" {
		t.Errorf("alias after |&: %q", out)
	}
	t.Setenv("GISH_ALIAS_VAR", "val")
	setAlias("gishv", "echo $GISH_ALIAS_VAR")
	defer unsetAlias("gishv")
	if out, _ := runLine(t, "gishv"); out != "val\n" {
		t.Errorf("alias body vars should expand: %q", out)
	}
}
