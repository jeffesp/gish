package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempDir creates a temp dir populated with the given file/dir names
// (a trailing "/" makes it a directory), chdirs into it for the duration
// of the test, and restores the original working directory on cleanup.
func withTempDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if name == "" {
			continue
		}
		if name[len(name)-1] == '/' {
			if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) }) //nolint:errcheck

	return dir
}

func TestWordAtCursor(t *testing.T) {
	cases := []struct {
		line      string
		pos       int
		wantStart int
		wantText  string
		wantQuote byte
		wantCmd   bool
	}{
		{"", 0, 0, "", 0, true},
		{"ec", 2, 0, "ec", 0, true},
		{"echo hel", 8, 5, "hel", 0, false},
		{"echo ", 5, 5, "", 0, false},
		{"cat file.out | gr", 17, 15, "gr", 0, true},
		{"cat file.out |& gr", 18, 16, "gr", 0, true},
		{"echo 'hello wor", 15, 5, "hello wor", '\'', false},
		{`echo "hello wor`, 15, 5, "hello wor", '"', false},
		{`echo a\ b`, 9, 5, "a b", 0, false},
		{"echo one two", 8, 5, "one", 0, false},
		// cursor in the middle of a word only sees the prefix before it
		{"echo hello", 7, 5, "he", 0, false},
		// cursor right after a completed pipe, before typing anything
		{"cat file.out |", 14, 14, "", 0, true},
	}

	for _, c := range cases {
		got := wordAtCursor(c.line, c.pos)
		if got.Start != c.wantStart || got.Text != c.wantText || got.Quote != c.wantQuote || got.IsCommand != c.wantCmd {
			t.Errorf("wordAtCursor(%q, %d) = %+v, want {Start:%d Text:%q Quote:%q IsCommand:%v}",
				c.line, c.pos, got, c.wantStart, c.wantText, c.wantQuote, c.wantCmd)
		}
	}
}

func TestFileCandidates(t *testing.T) {
	withTempDir(t, "file.txt", "file.out", "folder/", ".hidden")

	cases := []struct {
		partial string
		want    []string
	}{
		{"fi", []string{"file.out", "file.txt"}},
		{"file.t", []string{"file.txt"}},
		{"fo", []string{"folder/"}},
		{"", []string{"file.out", "file.txt", "folder/"}}, // .hidden excluded
		{".", []string{".hidden"}},
		{"nope", nil},
	}

	for _, c := range cases {
		got := fileCandidates(c.partial)
		if !equalStrings(got, c.want) {
			t.Errorf("fileCandidates(%q) = %v, want %v", c.partial, got, c.want)
		}
	}
}

func TestFileCandidatesSubdir(t *testing.T) {
	withTempDir(t, "folder/inner.txt", "folder/other.txt", "outside.txt")

	got := fileCandidates("folder/in")
	want := []string{"folder/inner.txt"}
	if !equalStrings(got, want) {
		t.Errorf("fileCandidates(folder/in) = %v, want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"file.txt"}, "file.txt"},
		{[]string{"file.txt", "file.out"}, "file."},
		{[]string{"foo", "bar"}, ""},
		{[]string{"folder/", "folder/"}, "folder/"},
	}
	for _, c := range cases {
		if got := commonPrefix(c.in); got != c.want {
			t.Errorf("commonPrefix(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCandidateList(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"file.out"}, "file.out\n"},
		{[]string{"file.out", "file.txt"}, "file.out\nfile.txt\n"},
	}
	for _, c := range cases {
		if got := candidateList(c.in); got != c.want {
			t.Errorf("candidateList(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestColumnate(t *testing.T) {
	entries := []string{"a", "bb", "ccc", "dddd", "e", "ff"}
	cases := []struct {
		name  string
		width int
		want  string
	}{
		{"unknown width falls back to one per line", 0, "a\nbb\nccc\ndddd\ne\nff\n"},
		{"too narrow for even one column falls back", 1, "a\nbb\nccc\ndddd\ne\nff\n"},
		// down-then-across into 2 rows of 3 cols: {a,dddd} {ccc,e} {... }
		{"fits three columns", 12, "a   ccc   e\nbb  dddd  ff\n"},
		// down-then-across into 4 rows of 2 cols: {a,bb,ccc,dddd} {e,ff}
		{"fits two columns", 8, "a     e\nbb    ff\nccc\ndddd\n"},
		{"fits one column", 4, "a\nbb\nccc\ndddd\ne\nff\n"},
	}
	for _, c := range cases {
		if got := columnate(entries, c.width); got != c.want {
			t.Errorf("%s: columnate(_, %d) = %q, want %q", c.name, c.width, got, c.want)
		}
	}
}

func TestColumnateEmpty(t *testing.T) {
	if got := columnate(nil, 80); got != "" {
		t.Errorf("columnate(nil, 80) = %q, want empty", got)
	}
}

func TestShellEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"file.txt", "file.txt"},
		{"a b", `a\ b`},
		{"a|b", `a\|b`},
		{`a"b`, `a\"b`},
	}
	for _, c := range cases {
		if got := shellEscape(c.in); got != c.want {
			t.Errorf("shellEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCompleteLine(t *testing.T) {
	withTempDir(t, "file.txt", "folder/inner.txt")

	cases := []struct {
		name     string
		line     string
		pos      int
		wantLine string
		wantPos  int
		wantOK   bool
	}{
		{"unique file completes with trailing space", "cat fi", 6, "cat file.txt ", 13, true},
		{"unique dir completes without trailing space", "cat fo", 6, "cat folder/", 11, true},
		{"completing inside a dir keeps going", "cat folder/in", 13, "cat folder/inner.txt ", 21, true},
		{"no matches leaves line untouched", "cat zz", 6, "", 0, false},
		{"non-tab key is ignored", "cat fi", 6, "", 0, false},
	}

	for _, c := range cases {
		key := rune('\t')
		if c.name == "non-tab key is ignored" {
			key = 'a'
		}
		gotLine, gotPos, gotOK := completeLine(c.line, c.pos, key)
		if gotOK != c.wantOK || (c.wantOK && (gotLine != c.wantLine || gotPos != c.wantPos)) {
			t.Errorf("%s: completeLine(%q, %d) = (%q, %d, %v), want (%q, %d, %v)",
				c.name, c.line, c.pos, gotLine, gotPos, gotOK, c.wantLine, c.wantPos, c.wantOK)
		}
	}
}

func TestCompleteLinePartialExtend(t *testing.T) {
	withTempDir(t, "file.txt", "file.out")

	// Ambiguous, but the shared "file." prefix is longer than what was
	// typed, so completion should advance to it and leave the word open
	// for another Tab rather than doing nothing.
	gotLine, gotPos, ok := completeLine("cat fi", 6, '\t')
	wantLine := "cat file."
	if !ok || gotLine != wantLine {
		t.Errorf(`completeLine("cat fi") = (%q, %d, %v), want (%q, _, true)`, gotLine, gotPos, ok, wantLine)
	}
}

func TestEraseLinesBelow(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, ""},
		{1, "\x1b[1B\x1b[1M\x1b[1A"},
		{2, "\x1b[1B\x1b[2M\x1b[1A"},
	}
	for _, c := range cases {
		var b strings.Builder
		eraseLinesBelow(&b, c.n)
		if got := b.String(); got != c.want {
			t.Errorf("eraseLinesBelow(_, %d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestPrintCandidatesBelow(t *testing.T) {
	withTempDir(t, "pfile.txt", "proj/")

	var out strings.Builder
	completionOut = &out
	completionPrompt = "gish> "
	lastListingLines = 0
	t.Cleanup(func() {
		completionOut = nil
		completionPrompt = ""
		lastListingLines = 0
	})

	line := "cat p"
	// "p" is already the shared prefix of both matches, so there's
	// nothing to extend — the candidates should be listed instead, on
	// fresh rows below, with the cursor back at len("gish> ")+len("cat
	// p") = 11 columns in on the (untouched) prompt row.
	gotLine, _, ok := completeLine(line, len(line), '\t')
	if ok {
		t.Errorf(`completeLine(%q) = (%q, _, true), want ok=false (ambiguous)`, line, gotLine)
	}

	want := "\r\npfile.txt\r\nproj/\r\n\x1b[3A\x1b[11C"
	if out.String() != want {
		t.Errorf("printed = %q, want %q", out.String(), want)
	}
	if lastListingLines != 2 {
		t.Errorf("lastListingLines = %d, want 2", lastListingLines)
	}

	// A second Tab on the same still-ambiguous word should erase the
	// previous listing's 2 lines before printing the new one in its
	// place, rather than stacking a second copy underneath.
	out.Reset()
	completeLine(line, len(line), '\t')
	wantSecond := "\x1b[1B\x1b[2M\x1b[1A" + want
	if out.String() != wantSecond {
		t.Errorf("second printed = %q, want %q", out.String(), wantSecond)
	}
}

func TestClearListing(t *testing.T) {
	var out strings.Builder
	completionOut = &out
	t.Cleanup(func() { completionOut = nil; lastListingLines = 0 })

	// Nothing to clear: a no-op.
	lastListingLines = 0
	clearListing()
	if out.Len() != 0 {
		t.Errorf("clearListing with nothing listed wrote %q, want nothing", out.String())
	}

	lastListingLines = 3
	clearListing()
	want := "\x1b[1B\x1b[3M\x1b[1A"
	if out.String() != want {
		t.Errorf("clearListing erase = %q, want %q", out.String(), want)
	}
	if lastListingLines != 0 {
		t.Errorf("lastListingLines = %d after clearListing, want 0", lastListingLines)
	}
}

// TestCompleteLineClearsStaleListing checks that a listing left on
// screen from a previous Tab gets cleared no matter what the current
// Tab does instead of replacing it with a new one: the word has since
// become a unique match, it's extended to a longer (still ambiguous)
// prefix, or it's still ambiguous with the exact same prefix as before.
func TestCompleteLineClearsStaleListing(t *testing.T) {
	var out strings.Builder
	completionOut = &out
	completionPrompt = "gish> "
	t.Cleanup(func() {
		completionOut = nil
		completionPrompt = ""
		lastListingLines = 0
	})

	cases := []struct {
		name  string
		files []string
		line  string
	}{
		{"becomes unique", []string{"pfile.txt", "proj/"}, "cat pf"},
		{"extends to a longer ambiguous prefix", []string{"afile.txt", "afolder/"}, "cat a"},
		{"still ambiguous with the same prefix", []string{"pfile.txt", "proj/"}, "cat p"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withTempDir(t, c.files...)
			lastListingLines = 2
			out.Reset()

			completeLine(c.line, len(c.line), '\t')

			wantErase := "\x1b[1B\x1b[2M\x1b[1A"
			if got := out.String(); !strings.HasPrefix(got, wantErase) {
				t.Errorf("completeLine(%q) wrote %q, want it to start with the erase sequence %q", c.line, got, wantErase)
			}
		})
	}
}

func TestCompleteLineQuoted(t *testing.T) {
	withTempDir(t, "file with space.txt")

	line := `cat "file`
	gotLine, gotPos, gotOK := completeLine(line, len(line), '\t')
	wantLine := `cat "file with space.txt" `
	if !gotOK || gotLine != wantLine {
		t.Errorf("completeLine(%q) = (%q, %d, %v), want (%q, _, true)", line, gotLine, gotPos, gotOK, wantLine)
	}
}

func TestCompleteLineDirOnlyCommand(t *testing.T) {
	withTempDir(t, "pfile.txt", "proj/")

	// Plain arg completion sees both the file and the directory, so the
	// common prefix is just "p" — nothing new to insert.
	if line, _, ok := completeLine("cat p", 5, '\t'); ok {
		t.Errorf(`completeLine("cat p") = (%q, _, true), want ok=false (ambiguous)`, line)
	}

	// cd is dir-only, so the file is filtered out and "proj/" is unique.
	gotLine, gotPos, ok := completeLine("cd p", 4, '\t')
	wantLine := "cd proj/"
	if !ok || gotLine != wantLine {
		t.Errorf(`completeLine("cd p") = (%q, %d, %v), want (%q, _, true)`, gotLine, gotPos, ok, wantLine)
	}
}

func TestEnterFilterClearsListing(t *testing.T) {
	var out strings.Builder
	completionOut = &out
	t.Cleanup(func() { completionOut = nil; lastListingLines = 0 })

	cases := []struct {
		name string
		in   string
	}{
		{"carriage return", "x\r"},
		{"line feed", "x\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lastListingLines = 2
			out.Reset()

			f := &enterFilter{Reader: strings.NewReader(c.in)}
			buf := make([]byte, len(c.in))
			if _, err := f.Read(buf); err != nil {
				t.Fatalf("Read: %v", err)
			}

			want := "\x1b[1B\x1b[2M\x1b[1A"
			if out.String() != want {
				t.Errorf("erase = %q, want %q", out.String(), want)
			}
			if lastListingLines != 0 {
				t.Errorf("lastListingLines = %d, want 0", lastListingLines)
			}
		})
	}
}

func TestEnterFilterPassesBytesThrough(t *testing.T) {
	f := &enterFilter{Reader: strings.NewReader("ab\rc")}
	got := readAll(t, f)
	if string(got) != "ab\rc" {
		t.Errorf("got %q, want %q", got, "ab\rc")
	}
}

func TestCommandForWord(t *testing.T) {
	cases := []struct {
		line string
		pos  int
		want string
	}{
		{"cd p", 4, "cd"},
		{"p", 1, ""},
		{"cat file.out | grep f", 22, "grep"},
		{"cd  ", 4, "cd"},
	}
	for _, c := range cases {
		w := wordAtCursor(c.line, c.pos)
		if got := commandForWord(c.line, w); got != c.want {
			t.Errorf("commandForWord(%q, word at %d) = %q, want %q", c.line, c.pos, got, c.want)
		}
	}
}
