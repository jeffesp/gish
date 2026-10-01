package term

import (
	"io"
	"slices"
	"strings"
	"testing"
)

// newTestTerminal returns a Terminal wired to an in-memory pipe: writes to
// the returned io.Writer become ReadLine's input, and out captures
// everything the Terminal echoes back.
func newTestTerminal(prompt string) (tm *Terminal, in io.Writer, out *strings.Builder) {
	r, w := io.Pipe()
	out = &strings.Builder{}
	tm = NewTerminal(struct {
		io.Reader
		io.Writer
	}{r, out}, prompt)
	return tm, w, out
}

// sendAsync writes s to in on its own goroutine, since io.Pipe's Write
// blocks until ReadLine (running on the test's goroutine) reads it.
func sendAsync(in io.Writer, s string) {
	go io.WriteString(in, s) //nolint:errcheck
}

func TestReadLineBasic(t *testing.T) {
	tm, in, _ := newTestTerminal("p> ")

	sendAsync(in, "hello\r")
	line, err := tm.ReadLine()
	if err != nil || line != "hello" {
		t.Fatalf("ReadLine = (%q, %v), want (%q, nil)", line, err, "hello")
	}
}

func TestCtrlCAbortsLineWithoutEOF(t *testing.T) {
	tm, in, out := newTestTerminal("p> ")

	sendAsync(in, "echo hi\x03echo bye\r")
	line, err := tm.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine returned err=%v, want nil (Ctrl+C must not EOF)", err)
	}
	if line != "echo bye" {
		t.Fatalf("line = %q, want %q", line, "echo bye")
	}
	if !strings.Contains(out.String(), "^C") {
		t.Errorf("output %q does not contain the ^C echo", out.String())
	}

	// Regression: Ctrl+C used to return "", io.EOF directly from
	// readLine's decode loop, skipping the bookkeeping that advances
	// t.remainder past the consumed byte. That left the stale byte in
	// the buffer, so the very next ReadLine replayed it and returned
	// io.EOF again, forever. Confirm a second call works normally.
	sendAsync(in, "ok\r")
	line2, err2 := tm.ReadLine()
	if err2 != nil || line2 != "ok" {
		t.Fatalf("second ReadLine = (%q, %v), want (%q, nil)", line2, err2, "ok")
	}
}

func TestCtrlDOnEmptyLineIsEOF(t *testing.T) {
	tm, in, _ := newTestTerminal("p> ")

	sendAsync(in, "\x04")
	line, err := tm.ReadLine()
	if err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if line != "" {
		t.Fatalf("line = %q, want empty", line)
	}
}

func TestCtrlDMidLineDeletesForward(t *testing.T) {
	tm, in, _ := newTestTerminal("p> ")

	// "ab", Left (Ctrl+B), Ctrl+D (forward-deletes 'b'), Enter.
	sendAsync(in, "ab\x02\x04\r")
	line, err := tm.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if line != "a" {
		t.Fatalf("line = %q, want %q (Ctrl+D with text left should forward-delete, not EOF)", line, "a")
	}
}

func TestPreKeyCallbackFiresForBoundAndUnboundKeys(t *testing.T) {
	tm, in, _ := newTestTerminal("p> ")

	var seen []rune
	tm.PreKeyCallback = func(_ string, _ int, key rune) {
		seen = append(seen, key)
	}

	// 'x' is unbound (falls through to the default case); Ctrl+A (Home)
	// is a built-in binding that AutoCompleteCallback would never see.
	sendAsync(in, "x\x01\r")
	line, err := tm.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if line != "x" {
		t.Fatalf("line = %q, want %q", line, "x")
	}

	want := []rune{'x', keyHome, keyEnter}
	if !slices.Equal(seen, want) {
		t.Fatalf("PreKeyCallback saw keys %v, want %v", seen, want)
	}
}

func TestPreKeyCallbackSkipsPaste(t *testing.T) {
	tm, in, _ := newTestTerminal("p> ")

	calls := 0
	tm.PreKeyCallback = func(string, int, rune) { calls++ }

	// Bracketed paste of "ab", then a real typed 'c', then Enter.
	sendAsync(in, "\x1b[200~ab\x1b[201~c\r")
	line, err := tm.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if line != "abc" {
		t.Fatalf("line = %q, want %q", line, "abc")
	}
	// Only 'c' and Enter should reach PreKeyCallback; the pasted 'a'/'b'
	// (and the paste markers themselves) are not real keys.
	if calls != 2 {
		t.Fatalf("PreKeyCallback called %d times, want 2 (c, Enter)", calls)
	}
}

func TestReadPasswordDisablesCallbacks(t *testing.T) {
	tm, in, _ := newTestTerminal("p> ")

	var autoCalls, preCalls int
	tm.AutoCompleteCallback = func(string, int, rune) (string, int, bool) {
		autoCalls++
		return "", 0, false
	}
	tm.PreKeyCallback = func(string, int, rune) { preCalls++ }

	sendAsync(in, "sekrit\r")
	pw, err := tm.ReadPassword("pw> ")
	if err != nil {
		t.Fatalf("ReadPassword: %v", err)
	}
	if pw != "sekrit" {
		t.Fatalf("password = %q, want %q", pw, "sekrit")
	}
	if autoCalls != 0 || preCalls != 0 {
		t.Fatalf("callbacks fired during ReadPassword: autoCalls=%d preCalls=%d, want 0, 0", autoCalls, preCalls)
	}

	// Both should be back in effect for a normal ReadLine afterward.
	sendAsync(in, "y\r")
	line, err := tm.ReadLine()
	if err != nil || line != "y" {
		t.Fatalf("ReadLine after ReadPassword = (%q, %v), want (%q, nil)", line, err, "y")
	}
	if autoCalls == 0 || preCalls == 0 {
		t.Fatalf("callbacks not restored after ReadPassword: autoCalls=%d preCalls=%d, want >0, >0", autoCalls, preCalls)
	}
}

func TestCursorColumn(t *testing.T) {
	tm, in, _ := newTestTerminal("gish> ") // 6 columns

	type observation struct {
		key rune
		col int
	}
	var got []observation
	tm.PreKeyCallback = func(_ string, _ int, key rune) {
		got = append(got, observation{key, tm.CursorColumn()})
	}

	// Tab has no AutoCompleteCallback registered here, so it's a no-op
	// (not printable, nothing to insert) — included to confirm the
	// column doesn't drift when a key does nothing.
	sendAsync(in, "ab\tcd\r")
	line, err := tm.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if line != "abcd" {
		t.Fatalf("line = %q, want %q", line, "abcd")
	}

	want := []observation{
		{'a', 6},       // "gish> "
		{'b', 7},       // "gish> a"
		{'\t', 8},      // "gish> ab"
		{'c', 8},       // "gish> ab" (Tab was a no-op)
		{'d', 9},       // "gish> abc"
		{keyEnter, 10}, // "gish> abcd"
	}
	if len(got) != len(want) {
		t.Fatalf("got %d observations %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("observation %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
