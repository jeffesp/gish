package main

import (
	"os"
	"path/filepath"
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

func TestCompleteLineQuoted(t *testing.T) {
	withTempDir(t, "file with space.txt")

	line := `cat "file`
	gotLine, gotPos, gotOK := completeLine(line, len(line), '\t')
	wantLine := `cat "file with space.txt" `
	if !gotOK || gotLine != wantLine {
		t.Errorf("completeLine(%q) = (%q, %d, %v), want (%q, _, true)", line, gotLine, gotPos, gotOK, wantLine)
	}
}
