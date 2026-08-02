package main

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// TerminalState tracks the terminal modes a job turns on so the shell can put
// the terminal back the way it found it when the job leaves the foreground —
// whether it was backgrounded with Ctrl+Z or died without cleaning up after
// itself.  Programs like htop, vim and less switch to the alternate screen,
// hide the cursor and enable mouse reporting; without this the shell draws its
// prompt on top of the job's display and reads mouse escapes as input.
//
// It works by scanning the bytes flowing from the job's PTY for DEC private
// mode set (CSI ? Pm h) and reset (CSI ? Pm l) sequences.  Feed is incremental:
// sequences split across reads are held in a pending buffer until complete.
type TerminalState struct {
	mu      sync.Mutex
	modes   map[int]bool
	pending []byte
	scroll  bool // a scrolling region (DECSTBM) has been set
}

// decPrivateModes are the modes worth restoring.  Anything else a job sets is
// left alone: guessing wrong is worse than leaving it, and these are the modes
// that actually make a terminal unusable when they leak.
var decPrivateModes = map[int]bool{
	7:    true, // autowrap
	25:   true, // cursor visibility
	47:   true, // alternate screen (legacy)
	1000: true, // mouse: button events
	1002: true, // mouse: button + drag
	1003: true, // mouse: any motion
	1005: true, // mouse: UTF-8 encoding
	1006: true, // mouse: SGR encoding
	1015: true, // mouse: urxvt encoding
	1047: true, // alternate screen (legacy)
	1048: true, // save/restore cursor
	1049: true, // alternate screen + save cursor
	2004: true, // bracketed paste
}

// altScreenModes turn the alternate screen on.  1049 is what modern programs
// use; 47 and 1047 are kept for older ones.
var altScreenModes = []int{1049, 1047, 47}

// defaultOnModes are on in a freshly reset terminal, so "restoring" them means
// setting them rather than clearing them.
var defaultOnModes = map[int]bool{7: true, 25: true}

func NewTerminalState() *TerminalState {
	return &TerminalState{modes: make(map[int]bool)}
}

// Feed scans a chunk of job output for mode changes.  It never modifies or
// withholds the bytes — callers pass them through to the terminal unchanged.
func (ts *TerminalState) Feed(p []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	buf := p
	if len(ts.pending) > 0 {
		buf = append(ts.pending, p...)
		ts.pending = nil
	}

	for i := 0; i < len(buf); i++ {
		if buf[i] != 0x1b {
			continue
		}
		consumed, complete := ts.scanEscape(buf[i:])
		if !complete {
			// Truncated sequence: hold the tail so the next Feed can finish it.
			// Cap it so a stream of lone ESC bytes cannot grow without bound.
			tail := buf[i:]
			if len(tail) > 64 {
				tail = tail[:64]
			}
			ts.pending = append([]byte(nil), tail...)
			return
		}
		i += consumed - 1
	}
}

// scanEscape parses one escape sequence starting at buf[0] == ESC.  It returns
// the number of bytes the sequence occupies and whether it was complete.
func (ts *TerminalState) scanEscape(buf []byte) (int, bool) {
	if len(buf) < 2 {
		return 0, false
	}
	if buf[1] != '[' {
		// Not CSI. Two-byte escapes are self-contained; anything longer we do
		// not care about, so treat it as consumed.
		return 2, true
	}
	i := 2
	private := false
	if i < len(buf) && buf[i] == '?' {
		private = true
		i++
	}
	start := i
	for i < len(buf) && (buf[i] == ';' || (buf[i] >= '0' && buf[i] <= '9')) {
		i++
	}
	if i >= len(buf) {
		return 0, false
	}
	params := string(buf[start:i])
	final := buf[i]
	i++

	switch {
	case private && (final == 'h' || final == 'l'):
		ts.applyModes(params, final == 'h')
	case !private && final == 'r':
		// DECSTBM: a scrolling region was set (or reset, with no params).
		ts.scroll = params != ""
	}
	return i, true
}

func (ts *TerminalState) applyModes(params string, set bool) {
	for _, field := range strings.Split(params, ";") {
		if field == "" {
			continue
		}
		mode, err := strconv.Atoi(field)
		if err != nil || !decPrivateModes[mode] {
			continue
		}
		if set == defaultOnModes[mode] {
			// Back to the default; stop tracking it.
			delete(ts.modes, mode)
			continue
		}
		ts.modes[mode] = set
	}
}

// AltScreen reports whether the job currently has the terminal on the
// alternate screen buffer.
func (ts *TerminalState) AltScreen() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.altScreenLocked()
}

func (ts *TerminalState) altScreenLocked() bool {
	for _, mode := range altScreenModes {
		if ts.modes[mode] {
			return true
		}
	}
	return false
}

// LeaveSequence returns the bytes that undo everything the job changed, for
// writing to the real terminal when the job leaves the foreground.
//
// Order matters: mouse reporting and bracketed paste go first so no stray
// input is generated mid-restore, the cursor comes back before the screen
// switches, and leaving the alternate screen is last because that is what
// restores the shell's scrollback.
func (ts *TerminalState) LeaveSequence() []byte {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	var out bytes.Buffer
	if len(ts.modes) == 0 && !ts.scroll {
		return nil
	}
	out.WriteString("\x1b[0m")
	for _, mode := range ts.sortedModesLocked() {
		if isAltScreen(mode) {
			continue
		}
		out.WriteString(modeSequence(mode, defaultOnModes[mode]))
	}
	if ts.scroll {
		out.WriteString("\x1b[r")
	}
	for _, mode := range altScreenModes {
		if ts.modes[mode] {
			out.WriteString(modeSequence(mode, false))
		}
	}
	return out.Bytes()
}

// EnterSequence re-establishes the modes the job had set, for when it is
// brought back to the foreground.  It is the inverse of LeaveSequence: the
// alternate screen goes first so the job's display is restored before its
// cursor and input modes are put back.
func (ts *TerminalState) EnterSequence() []byte {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	var out bytes.Buffer
	for _, mode := range altScreenModes {
		if ts.modes[mode] {
			out.WriteString(modeSequence(mode, true))
		}
	}
	for _, mode := range ts.sortedModesLocked() {
		if isAltScreen(mode) {
			continue
		}
		out.WriteString(modeSequence(mode, ts.modes[mode]))
	}
	return out.Bytes()
}

// sortedModesLocked gives the tracked modes a stable order so the emitted
// sequences are deterministic and testable.
func (ts *TerminalState) sortedModesLocked() []int {
	modes := make([]int, 0, len(ts.modes))
	for mode := range ts.modes {
		modes = append(modes, mode)
	}
	sort.Ints(modes)
	return modes
}

func isAltScreen(mode int) bool {
	for _, alt := range altScreenModes {
		if mode == alt {
			return true
		}
	}
	return false
}

func modeSequence(mode int, set bool) string {
	final := "l"
	if set {
		final = "h"
	}
	return "\x1b[?" + strconv.Itoa(mode) + final
}
