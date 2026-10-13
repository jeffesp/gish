package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenizeRedirects(t *testing.T) {
	cases := []struct {
		in   string
		want []string // canonical operators appear as "R:op", others as the word
	}{
		{"echo hi > f", []string{"echo", "hi", "R:>", "f"}},
		{"echo hi >f", []string{"echo", "hi", "R:>", "f"}},
		{"echo hi >> f", []string{"echo", "hi", "R:>>", "f"}},
		{"ls 2> f", []string{"ls", "R:2>", "f"}},
		{"ls 2>/dev/null", []string{"ls", "R:2>", "/dev/null"}},
		{"ls 2>> f", []string{"ls", "R:2>>", "f"}},
		{"ls &> f", []string{"ls", "R:&>", "f"}},
		{"ls &>> f", []string{"ls", "R:&>>", "f"}},
		{"cat < f", []string{"cat", "R:<", "f"}},
		{"echo out> f", []string{"echo", "R:>", "f"}},
		{"echo out>> f", []string{"echo", "R:>>", "f"}},
		{"ls err> f", []string{"ls", "R:2>", "f"}},
		{"ls out+err> f", []string{"ls", "R:&>", "f"}},
		{"cat in< f", []string{"cat", "R:<", "f"}},
		// Not at a word boundary, quoted or escaped: ordinary text.
		{"echo a>b", []string{"echo", "a>b"}},
		{`echo ">"`, []string{"echo", ">"}},
		{`echo '>'`, []string{"echo", ">"}},
		{`echo \> x`, []string{"echo", ">", "x"}},
		{"echo 2 > f", []string{"echo", "2", "R:>", "f"}},
		{"echo output", []string{"echo", "output"}},
		{"ls a | cat > f", []string{"ls", "a", "|", "cat", "R:>", "f"}},
	}
	for _, c := range cases {
		var got []string
		for _, tok := range mustTokenize(t, c.in) {
			if tok.Kind == TokenRedirect {
				got = append(got, "R:"+tok.Value)
			} else {
				got = append(got, tok.Value)
			}
		}
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Errorf("tokenize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractRedirects(t *testing.T) {
	toks, rs, err := extractRedirects(mustTokenize(t, "a 2> e | b > o < i"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tokenValues(toks), " "); got != "a | b" {
		t.Errorf("remaining tokens = %q, want %q", got, "a | b")
	}
	if len(rs[0]) != 1 || rs[0][0] != (Redirect{RedirectErr, "e"}) {
		t.Errorf("stage 0 = %v", rs[0])
	}
	if len(rs[1]) != 2 || rs[1][0] != (Redirect{RedirectOut, "o"}) || rs[1][1] != (Redirect{RedirectIn, "i"}) {
		t.Errorf("stage 1 = %v", rs[1])
	}

	for _, bad := range []string{"echo >", "echo > > f", "echo > | cat", "echo > ;"} {
		toks := mustTokenize(t, bad)
		// split on lists the way execLine does so ';' is not a file name
		items, _ := splitList(toks)
		if len(items) > 0 {
			toks = items[0].Tokens
		}
		if _, _, err := extractRedirects(toks); err == nil {
			t.Errorf("extractRedirects(%q): expected error", bad)
		}
	}
}

func TestExecLineRedirects(t *testing.T) {
	historyFile = filepath.Join(t.TempDir(), "h")
	dir := t.TempDir()
	f := filepath.Join(dir, "out.txt")
	read := func(p string) string {
		b, _ := os.ReadFile(p)
		return string(b)
	}

	out, errOut := runLine(t, "echo hello > "+f)
	if out != "" || errOut != "" || read(f) != "hello\n" {
		t.Fatalf("overwrite: out=%q err=%q file=%q", out, errOut, read(f))
	}
	runLine(t, "echo world >> "+f)
	if read(f) != "hello\nworld\n" {
		t.Errorf("append: file=%q", read(f))
	}
	runLine(t, "echo again > "+f)
	if read(f) != "again\n" {
		t.Errorf("overwrite truncates: file=%q", read(f))
	}

	// stdin from file
	out, _ = runLine(t, "cat < "+f)
	if out != "again\n" {
		t.Errorf("cat < file = %q", out)
	}

	// stderr only
	ef := filepath.Join(dir, "err.txt")
	out, errOut = runLine(t, "ls /definitely/not/here 2> "+ef)
	if out != "" || strings.Contains(errOut, "cannot access") || !strings.Contains(read(ef), "definitely") {
		t.Errorf("2>: out=%q err=%q file=%q", out, errOut, read(ef))
	}

	// both streams
	bf := filepath.Join(dir, "both.txt")
	runLine(t, "ls /definitely/not/here &> "+bf)
	if !strings.Contains(read(bf), "definitely") {
		t.Errorf("&>: file=%q", read(bf))
	}

	// named forms
	nf := filepath.Join(dir, "named.txt")
	runLine(t, "echo named out> "+nf)
	if read(nf) != "named\n" {
		t.Errorf("out>: file=%q", read(nf))
	}

	// last redirect to a stream wins
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	runLine(t, "echo x > "+a+" > "+b)
	if read(b) != "x\n" {
		t.Errorf("last wins: b=%q", read(b))
	}
}

func TestExecLineRedirectsInPipelines(t *testing.T) {
	historyFile = filepath.Join(t.TempDir(), "h")
	dir := t.TempDir()
	f := filepath.Join(dir, "o")

	// Redirected stdout means the next stage sees nothing.
	out, _ := runLine(t, "echo hi > "+f+" | cat")
	if out != "" || strings.TrimSpace(func() string { b, _ := os.ReadFile(f); return string(b) }()) != "hi" {
		t.Errorf("redirect then pipe: out=%q", out)
	}

	// Stderr of one stage is discarded, stdout still flows.
	out, errOut := runLine(t, "ls /definitely/not/here 2> "+filepath.Join(dir, "e")+" | cat")
	if out != "" || strings.Contains(errOut, "cannot access") {
		t.Errorf("2> in pipeline: out=%q err=%q", out, errOut)
	}

	// Last stage redirected.
	runLine(t, "echo piped | cat > "+f)
	if b, _ := os.ReadFile(f); string(b) != "piped\n" {
		t.Errorf("pipeline > file: %q", b)
	}

	// Input redirect on the first stage.
	runLine(t, "echo src > "+f)
	out, _ = runLine(t, "cat < "+f+" | cat")
	if out != "src\n" {
		t.Errorf("< into pipeline: %q", out)
	}
}

func TestExecLineRedirectErrors(t *testing.T) {
	historyFile = filepath.Join(t.TempDir(), "h")
	out, errOut := runLine(t, "echo hi >")
	if out != "" || !strings.Contains(errOut, "needs a file name") {
		t.Errorf("missing name: out=%q err=%q", out, errOut)
	}
	_, errOut = runLine(t, "> f")
	if !strings.Contains(errOut, "without a command") {
		t.Errorf("no command: err=%q", errOut)
	}
	_, errOut = runLine(t, "cat < /definitely/not/here")
	if !strings.Contains(errOut, "no such file") {
		t.Errorf("missing input: err=%q", errOut)
	}
	// $? reflects the failed redirect
	if out, _ := runLine(t, "cat < /definitely/not/here; echo $?"); out != "1\n" {
		t.Errorf("$? after failed redirect = %q", out)
	}
}
