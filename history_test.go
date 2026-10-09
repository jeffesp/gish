package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withTestHistory(t *testing.T) (string, func()) {
	t.Helper()
	origFile := historyFile
	origMax := historyMax
	tmp := filepath.Join(t.TempDir(), "hist")
	historyFile = tmp
	historyMax = 0
	origSession := sessionID
	sessionID = "1"
	return tmp, func() {
		historyFile = origFile
		historyMax = origMax
		sessionID = origSession
	}
}

func TestAppendAndLoadHistory(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	e := HistoryEntry{
		Command:   "echo hello",
		Dir:       "/tmp",
		ExitCode:  0,
		StartTime: now,
		EndTime:   now.Add(100 * time.Millisecond),
		SessionID: "12345",
	}
	appendHistory(e)
	appendHistory(HistoryEntry{
		Command:   "ls -la",
		Dir:       "/home",
		ExitCode:  1,
		StartTime: now.Add(time.Second),
		EndTime:   now.Add(2 * time.Second),
		SessionID: "12345",
	})

	entries, err := loadHistory()
	if err != nil {
		t.Fatalf("loadHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Command != "echo hello" {
		t.Errorf("entry[0].Command = %q, want %q", entries[0].Command, "echo hello")
	}
	if entries[1].ExitCode != 1 {
		t.Errorf("entry[1].ExitCode = %d, want 1", entries[1].ExitCode)
	}
}

func TestLoadHistorySkipsMalformed(t *testing.T) {
	tmp, cleanup := withTestHistory(t)
	defer cleanup()

	content := `{"cmd":"good","dir":"/tmp","exit":0,"start":"2026-01-01T00:00:00Z","end":"2026-01-01T00:00:01Z","session":"1"}
not json at all
{"cmd":"also good","dir":"/tmp","exit":0,"start":"2026-01-02T00:00:00Z","end":"2026-01-02T00:00:01Z","session":"1"}
`
	os.WriteFile(tmp, []byte(content), 0600)

	entries, err := loadHistory()
	if err != nil {
		t.Fatalf("loadHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (malformed line should be skipped)", len(entries))
	}
}

func TestLoadHistoryMissingFile(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	entries, err := loadHistory()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestTruncateHistory(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	for i := range 10 {
		appendHistory(HistoryEntry{
			Command:   fmt.Sprintf("cmd %d", i),
			Dir:       "/tmp",
			ExitCode:  0,
			StartTime: now.Add(time.Duration(i) * time.Second),
			EndTime:   now.Add(time.Duration(i)*time.Second + time.Millisecond),
			SessionID: "1",
		})
	}

	historyMax = 3
	truncateHistory()

	entries, err := loadHistory()
	if err != nil {
		t.Fatalf("loadHistory: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries after truncation, want 3", len(entries))
	}
	if entries[0].Command != "cmd 7" {
		t.Errorf("first entry after truncation: %q, want %q", entries[0].Command, "cmd 7")
	}
}

func TestTruncateHistoryNoOpWhenUnlimited(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	for i := range 5 {
		appendHistory(HistoryEntry{
			Command:   fmt.Sprintf("cmd %d", i),
			Dir:       "/tmp",
			ExitCode:  0,
			StartTime: now,
			EndTime:   now,
			SessionID: "1",
		})
	}

	historyMax = 0
	truncateHistory()

	entries, _ := loadHistory()
	if len(entries) != 5 {
		t.Fatalf("truncation with max=0 should be no-op; got %d entries, want 5", len(entries))
	}
}

func TestExitCode(t *testing.T) {
	if got := exitCode(nil); got != 0 {
		t.Errorf("exitCode(nil) = %d, want 0", got)
	}

	if got := exitCode(fmt.Errorf("exec not found")); got != 1 {
		t.Errorf("exitCode(generic error) = %d, want 1", got)
	}

	// Produce a real ExitError by running a command that exits non-zero
	cmd := exec.Command("sh", "-c", "exit 42")
	err := cmd.Run()
	if got := exitCode(err); got != 42 {
		t.Errorf("exitCode(exit 42) = %d, want 42", got)
	}
}

func TestBuiltinHistoryNoArgs(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Date(2026, 4, 15, 10, 0, 0, 0, time.Local)
	appendHistory(HistoryEntry{
		Command:   "echo hello",
		Dir:       "/tmp",
		ExitCode:  0,
		StartTime: now,
		EndTime:   now.Add(50 * time.Millisecond),
		SessionID: "1",
	})

	var buf bytes.Buffer
	if err := builtinHistory(testCmd([]Token{}), testCtx(&buf)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "echo hello") {
		t.Errorf("output missing command: %q", out)
	}
	if strings.Contains(out, "/tmp") || strings.Contains(out, "2026") {
		t.Errorf("default output should omit dir and time: %q", out)
	}
	if fields := strings.Fields(out); len(fields) < 4 || fields[0] != "1" || fields[1] != "0" {
		t.Errorf("want number, exit code, command: %q", out)
	}
}

func TestBuiltinHistoryLong(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Date(2026, 4, 15, 10, 0, 0, 0, time.Local)
	appendHistory(HistoryEntry{Command: "echo hello", Dir: "/tmp", ExitCode: 0,
		StartTime: now, EndTime: now.Add(50 * time.Millisecond), SessionID: "1"})

	var buf bytes.Buffer
	if err := builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "--long"}}), testCtx(&buf)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"echo hello", "/tmp", "exit=0", "dur=50ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("--long output missing %q: %q", want, out)
		}
	}
}

func TestBuiltinHistorySessionScope(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	appendHistory(HistoryEntry{Command: "other session", ExitCode: 0, StartTime: now, EndTime: now, SessionID: "2"})
	appendHistory(HistoryEntry{Command: "this session", ExitCode: 3, StartTime: now, EndTime: now, SessionID: "1"})

	var buf bytes.Buffer
	if err := builtinHistory(testCmd(nil), testCtx(&buf)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "other session") || !strings.Contains(out, "this session") {
		t.Errorf("default should show only current session: %q", out)
	}
	// Number must match the position in the full history so !N works.
	if fields := strings.Fields(out); fields[0] != "2" {
		t.Errorf("number should be global index 2, got: %q", out)
	}

	buf.Reset()
	if err := builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "--all"}}), testCtx(&buf)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "other session") {
		t.Errorf("--all should include other sessions: %q", buf.String())
	}
}

func TestBuiltinHistoryWithLimit(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	for i := range 10 {
		appendHistory(HistoryEntry{
			Command:   fmt.Sprintf("cmd %d", i),
			Dir:       "/tmp",
			ExitCode:  0,
			StartTime: now.Add(time.Duration(i) * time.Second),
			EndTime:   now.Add(time.Duration(i)*time.Second + time.Millisecond),
			SessionID: "1",
		})
	}

	var buf bytes.Buffer
	if err := builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "3"}}), testCtx(&buf)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if !strings.Contains(lines[0], "cmd 7") {
		t.Errorf("first line should be cmd 7, got: %s", lines[0])
	}
}

func TestBuiltinHistorySince(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	appendHistory(HistoryEntry{
		Command:   "old cmd",
		Dir:       "/tmp",
		ExitCode:  0,
		StartTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local),
		EndTime:   time.Date(2026, 1, 1, 12, 0, 1, 0, time.Local),
		SessionID: "1",
	})
	appendHistory(HistoryEntry{
		Command:   "new cmd",
		Dir:       "/tmp",
		ExitCode:  0,
		StartTime: time.Date(2026, 4, 15, 12, 0, 0, 0, time.Local),
		EndTime:   time.Date(2026, 4, 15, 12, 0, 1, 0, time.Local),
		SessionID: "1",
	})

	var buf bytes.Buffer
	err := builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "--since"}, {Kind: TokenWord, Value: "2026-04-01"}}), testCtx(&buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "old cmd") {
		t.Errorf("should not contain old cmd: %q", out)
	}
	if !strings.Contains(out, "new cmd") {
		t.Errorf("should contain new cmd: %q", out)
	}
}

func TestBuiltinHistoryUntil(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	appendHistory(HistoryEntry{
		Command:   "early cmd",
		Dir:       "/tmp",
		ExitCode:  0,
		StartTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local),
		EndTime:   time.Date(2026, 1, 1, 12, 0, 1, 0, time.Local),
		SessionID: "1",
	})
	appendHistory(HistoryEntry{
		Command:   "late cmd",
		Dir:       "/tmp",
		ExitCode:  0,
		StartTime: time.Date(2026, 6, 1, 12, 0, 0, 0, time.Local),
		EndTime:   time.Date(2026, 6, 1, 12, 0, 1, 0, time.Local),
		SessionID: "1",
	})

	var buf bytes.Buffer
	err := builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "--until"}, {Kind: TokenWord, Value: "2026-03-01"}}), testCtx(&buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "early cmd") {
		t.Errorf("should contain early cmd: %q", out)
	}
	if strings.Contains(out, "late cmd") {
		t.Errorf("should not contain late cmd: %q", out)
	}
}

func TestBuiltinHistoryOkAndFail(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	appendHistory(HistoryEntry{
		Command: "good cmd", Dir: "/tmp", ExitCode: 0,
		StartTime: now, EndTime: now, SessionID: "1",
	})
	appendHistory(HistoryEntry{
		Command: "bad cmd", Dir: "/tmp", ExitCode: 1,
		StartTime: now, EndTime: now, SessionID: "1",
	})

	var buf bytes.Buffer
	err := builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "--ok"}}), testCtx(&buf))
	if err != nil {
		t.Fatalf("--ok: unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "good cmd") {
		t.Errorf("--ok should include good cmd")
	}
	if strings.Contains(out, "bad cmd") {
		t.Errorf("--ok should exclude bad cmd")
	}

	buf.Reset()
	err = builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "--fail"}}), testCtx(&buf))
	if err != nil {
		t.Fatalf("--fail: unexpected error: %v", err)
	}
	out = buf.String()
	if strings.Contains(out, "good cmd") {
		t.Errorf("--fail should exclude good cmd")
	}
	if !strings.Contains(out, "bad cmd") {
		t.Errorf("--fail should include bad cmd")
	}
}

func TestBuiltinHistoryOkFailMutuallyExclusive(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	var buf bytes.Buffer
	err := builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "--ok"}, {Kind: TokenWord, Value: "--fail"}}), testCtx(&buf))
	if err == nil {
		t.Error("expected error for --ok --fail together")
	}
}

func TestBuiltinHistoryDirFilter(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	appendHistory(HistoryEntry{
		Command: "in project", Dir: "/Users/jeff/Code/gish", ExitCode: 0,
		StartTime: now, EndTime: now, SessionID: "1",
	})
	appendHistory(HistoryEntry{
		Command: "at home", Dir: "/Users/jeff", ExitCode: 0,
		StartTime: now, EndTime: now, SessionID: "1",
	})

	var buf bytes.Buffer
	err := builtinHistory(testCmd([]Token{{Kind: TokenWord, Value: "--dir"}, {Kind: TokenWord, Value: "gish"}}), testCtx(&buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "in project") {
		t.Errorf("should contain 'in project'")
	}
	if strings.Contains(out, "at home") {
		t.Errorf("should not contain 'at home'")
	}
}

func TestBuiltinHistoryCombinedFilters(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	base := time.Date(2026, 4, 15, 10, 0, 0, 0, time.Local)
	entries := []HistoryEntry{
		{Command: "old fail", Dir: "/project", ExitCode: 1,
			StartTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local),
			EndTime:   time.Date(2026, 1, 1, 12, 0, 1, 0, time.Local), SessionID: "1"},
		{Command: "recent fail", Dir: "/project", ExitCode: 1,
			StartTime: base, EndTime: base.Add(time.Second), SessionID: "1"},
		{Command: "recent ok", Dir: "/project", ExitCode: 0,
			StartTime: base.Add(time.Second), EndTime: base.Add(2 * time.Second), SessionID: "1"},
		{Command: "recent fail other dir", Dir: "/other", ExitCode: 1,
			StartTime: base.Add(2 * time.Second), EndTime: base.Add(3 * time.Second), SessionID: "1"},
	}
	for _, e := range entries {
		appendHistory(e)
	}

	var buf bytes.Buffer
	args := []Token{
		{Kind: TokenWord, Value: "--since"}, {Kind: TokenWord, Value: "2026-04-01"},
		{Kind: TokenWord, Value: "--fail"},
		{Kind: TokenWord, Value: "--dir"}, {Kind: TokenWord, Value: "/project"},
	}
	err := builtinHistory(testCmd(args), testCtx(&buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 result, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "recent fail") {
		t.Errorf("expected 'recent fail', got: %s", lines[0])
	}
}

func TestTermHistorySeeding(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	for i := range 5 {
		appendHistory(HistoryEntry{
			Command: fmt.Sprintf("cmd %d", i), Dir: "/tmp", ExitCode: 0,
			StartTime: now, EndTime: now, SessionID: "1",
		})
	}

	h := newTermHistory()
	if h.Len() != 5 {
		t.Fatalf("Len() = %d, want 5", h.Len())
	}
	if got := h.At(0); got != "cmd 4" {
		t.Errorf("At(0) = %q, want %q", got, "cmd 4")
	}
	if got := h.At(4); got != "cmd 0" {
		t.Errorf("At(4) = %q, want %q", got, "cmd 0")
	}
}

// TestTermHistoryAtBounds verifies At returns "" for out-of-range
// indices instead of panicking: previously idx == Len() indexed
// entries at -1.
func TestTermHistoryAtBounds(t *testing.T) {
	h := &termHistory{entries: []string{"old", "new"}}
	if got := h.At(0); got != "new" {
		t.Errorf("At(0) = %q, want %q", got, "new")
	}
	if got := h.At(1); got != "old" {
		t.Errorf("At(1) = %q, want %q", got, "old")
	}
	if got := h.At(2); got != "" {
		t.Errorf("At(2) = %q, want empty (idx == Len)", got)
	}
	if got := h.At(-1); got != "" {
		t.Errorf("At(-1) = %q, want empty", got)
	}
}

func TestTermHistorySeedingCapsAtMax(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	for i := range 150 {
		appendHistory(HistoryEntry{
			Command: fmt.Sprintf("cmd %d", i), Dir: "/tmp", ExitCode: 0,
			StartTime: now, EndTime: now, SessionID: "1",
		})
	}

	h := newTermHistory()
	if h.Len() != termHistorySize {
		t.Fatalf("Len() = %d, want %d", h.Len(), termHistorySize)
	}
	if got := h.At(0); got != "cmd 149" {
		t.Errorf("At(0) = %q, want %q", got, "cmd 149")
	}
}

func TestTermHistoryAdd(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	h := newTermHistory()
	h.Add("new cmd")
	if h.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", h.Len())
	}
	if got := h.At(0); got != "new cmd" {
		t.Errorf("At(0) = %q, want %q", got, "new cmd")
	}
}

func TestExpandHistoryBang(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	now := time.Now()
	appendHistory(HistoryEntry{
		Command: "echo hello", Dir: "/tmp", ExitCode: 0,
		StartTime: now, EndTime: now, SessionID: "1",
	})
	appendHistory(HistoryEntry{
		Command: "ls -la", Dir: "/tmp", ExitCode: 0,
		StartTime: now, EndTime: now, SessionID: "1",
	})

	// !! should return last command
	expanded, ok := expandHistory("!!")
	if !ok {
		t.Fatal("!! should expand")
	}
	if expanded != "ls -la" {
		t.Errorf("!! = %q, want %q", expanded, "ls -la")
	}

	// !echo should match "echo hello"
	expanded, ok = expandHistory("!echo")
	if !ok {
		t.Fatal("!echo should expand")
	}
	if expanded != "echo hello" {
		t.Errorf("!echo = %q, want %q", expanded, "echo hello")
	}

	// !ls should match "ls -la" (most recent)
	expanded, ok = expandHistory("!ls")
	if !ok {
		t.Fatal("!ls should expand")
	}
	if expanded != "ls -la" {
		t.Errorf("!ls = %q, want %q", expanded, "ls -la")
	}

	// !1 should rerun the first command
	expanded, ok = expandHistory("!1")
	if !ok {
		t.Fatal("!1 should expand")
	}
	if expanded != "echo hello" {
		t.Errorf("!1 = %q, want %q", expanded, "echo hello")
	}

	// !23 should not expand
	_, ok = expandHistory("!23")
	if ok {
		t.Fatal("!23 should not expand, outside history bounds")
	}

	// !nonexistent should not expand
	_, ok = expandHistory("!nonexistent")
	if ok {
		t.Error("!nonexistent should not expand")
	}

	// Regular line should not expand
	_, ok = expandHistory("echo hello")
	if ok {
		t.Error("regular line should not expand")
	}

	// "! cmd" (space after !) should not expand
	_, ok = expandHistory("! echo")
	if ok {
		t.Error("'! echo' should not expand")
	}
}

func TestExpandHistoryEmptyHistory(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	_, ok := expandHistory("!!")
	if ok {
		t.Error("!! with empty history should not expand")
	}
}

func TestBuiltinHistoryBadArgs(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	var buf bytes.Buffer

	cases := []struct {
		name string
		args []Token
	}{
		{"negative N", []Token{{Kind: TokenWord, Value: "-5"}}},
		{"zero N", []Token{{Kind: TokenWord, Value: "0"}}},
		{"non-numeric N", []Token{{Kind: TokenWord, Value: "abc"}}},
		{"since no value", []Token{{Kind: TokenWord, Value: "--since"}}},
		{"until no value", []Token{{Kind: TokenWord, Value: "--until"}}},
		{"dir no value", []Token{{Kind: TokenWord, Value: "--dir"}}},
		{"since bad date", []Token{{Kind: TokenWord, Value: "--since"}, {Kind: TokenWord, Value: "not-a-date"}}},
		{"unknown flag", []Token{{Kind: TokenWord, Value: "--bogus"}}},
	}

	for _, c := range cases {
		buf.Reset()
		if err := builtinHistory(testCmd(c.args), testCtx(&buf)); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func TestBuiltinHistoryHelp(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	for _, flag := range []string{"--help", "-h"} {
		var buf bytes.Buffer
		args := []Token{{Kind: TokenWord, Value: flag}}
		if err := builtinHistory(testCmd(args), testCtx(&buf)); err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		for _, want := range []string{"usage: history", "--all", "--long", "--since", "--until", "--ok", "--fail", "--dir"} {
			if !strings.Contains(buf.String(), want) {
				t.Errorf("%s: help missing %q:\n%s", flag, want, buf.String())
			}
		}
	}
}
