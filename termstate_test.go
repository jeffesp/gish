package main

import (
	"strings"
	"testing"
)

func TestTerminalStateTracksAltScreen(t *testing.T) {
	ts := NewTerminalState()
	if ts.AltScreen() {
		t.Fatal("fresh state should not report the alternate screen")
	}
	ts.Feed([]byte("\x1b[?1049h hello"))
	if !ts.AltScreen() {
		t.Fatal("expected alternate screen after ?1049h")
	}
	ts.Feed([]byte("\x1b[?1049l"))
	if ts.AltScreen() {
		t.Fatal("expected alternate screen cleared after ?1049l")
	}
}

func TestTerminalStateLeaveSequenceRestoresModes(t *testing.T) {
	ts := NewTerminalState()
	ts.Feed([]byte("\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?2004h"))

	leave := string(ts.LeaveSequence())
	for _, want := range []string{"\x1b[?25h", "\x1b[?1000l", "\x1b[?2004l", "\x1b[?1049l"} {
		if !strings.Contains(leave, want) {
			t.Errorf("leave sequence %q missing %q", leave, want)
		}
	}

	// Mouse reporting and bracketed paste must be turned off before the screen
	// switches back, or the terminal can emit input mid-restore.
	if strings.Index(leave, "\x1b[?1000l") > strings.Index(leave, "\x1b[?1049l") {
		t.Error("mouse reporting should be disabled before leaving the alternate screen")
	}
	if strings.Index(leave, "\x1b[?25h") > strings.Index(leave, "\x1b[?1049l") {
		t.Error("cursor should be restored before leaving the alternate screen")
	}
}

func TestTerminalStateLeaveSequenceEmptyWhenClean(t *testing.T) {
	ts := NewTerminalState()
	ts.Feed([]byte("just some plain output\r\n"))
	if seq := ts.LeaveSequence(); len(seq) != 0 {
		t.Errorf("expected no restore for a well-behaved job, got %q", seq)
	}
}

func TestTerminalStateLeaveSequenceIgnoresBalancedModes(t *testing.T) {
	ts := NewTerminalState()
	ts.Feed([]byte("\x1b[?1049h\x1b[?25l"))
	ts.Feed([]byte("\x1b[?25h\x1b[?1049l"))
	if seq := ts.LeaveSequence(); len(seq) != 0 {
		t.Errorf("a job that cleaned up after itself needs no restore, got %q", seq)
	}
}

func TestTerminalStateEnterSequenceIsInverse(t *testing.T) {
	ts := NewTerminalState()
	ts.Feed([]byte("\x1b[?1049h\x1b[?25l\x1b[?1002h"))

	enter := string(ts.EnterSequence())
	for _, want := range []string{"\x1b[?1049h", "\x1b[?25l", "\x1b[?1002h"} {
		if !strings.Contains(enter, want) {
			t.Errorf("enter sequence %q missing %q", enter, want)
		}
	}
	// The screen has to come back before the cursor and input modes are reapplied.
	if strings.Index(enter, "\x1b[?1049h") != 0 {
		t.Errorf("alternate screen should be restored first, got %q", enter)
	}
}

func TestTerminalStateHandlesSplitSequences(t *testing.T) {
	ts := NewTerminalState()
	// A read boundary can fall anywhere, including mid-escape-sequence.
	ts.Feed([]byte("\x1b[?10"))
	ts.Feed([]byte("49h"))
	if !ts.AltScreen() {
		t.Fatal("expected a sequence split across reads to be reassembled")
	}
}

func TestTerminalStateHandlesMultiParamSequences(t *testing.T) {
	ts := NewTerminalState()
	ts.Feed([]byte("\x1b[?1000;1006;2004h"))
	leave := string(ts.LeaveSequence())
	for _, want := range []string{"\x1b[?1000l", "\x1b[?1006l", "\x1b[?2004l"} {
		if !strings.Contains(leave, want) {
			t.Errorf("leave sequence %q missing %q", leave, want)
		}
	}
}

func TestTerminalStateResetsScrollRegion(t *testing.T) {
	ts := NewTerminalState()
	ts.Feed([]byte("\x1b[5;20r"))
	if seq := string(ts.LeaveSequence()); !strings.Contains(seq, "\x1b[r") {
		t.Errorf("expected scroll region reset, got %q", seq)
	}
}

func TestTerminalStatePendingBufferIsBounded(t *testing.T) {
	ts := NewTerminalState()
	// A job emitting lone ESC bytes must not make the tracker grow without bound.
	for i := 0; i < 500; i++ {
		ts.Feed([]byte("\x1b"))
	}
	ts.mu.Lock()
	pending := len(ts.pending)
	ts.mu.Unlock()
	if pending > 64 {
		t.Errorf("pending buffer grew to %d bytes", pending)
	}
}
