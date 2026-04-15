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
	return tmp, func() {
		historyFile = origFile
		historyMax = origMax
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
	if err := builtinHistory([]Token{}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "echo hello") {
		t.Errorf("output missing command: %q", out)
	}
	if !strings.Contains(out, "/tmp") {
		t.Errorf("output missing dir: %q", out)
	}
	if !strings.Contains(out, "exit=0") {
		t.Errorf("output missing exit code: %q", out)
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
	if err := builtinHistory([]Token{{TokenWord, "3"}}, &buf); err != nil {
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
	err := builtinHistory([]Token{{TokenWord, "--since"}, {TokenWord, "2026-04-01"}}, &buf)
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
	err := builtinHistory([]Token{{TokenWord, "--until"}, {TokenWord, "2026-03-01"}}, &buf)
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
	err := builtinHistory([]Token{{TokenWord, "--ok"}}, &buf)
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
	err = builtinHistory([]Token{{TokenWord, "--fail"}}, &buf)
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
	err := builtinHistory([]Token{{TokenWord, "--ok"}, {TokenWord, "--fail"}}, &buf)
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
	err := builtinHistory([]Token{{TokenWord, "--dir"}, {TokenWord, "gish"}}, &buf)
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
		{TokenWord, "--since"}, {TokenWord, "2026-04-01"},
		{TokenWord, "--fail"},
		{TokenWord, "--dir"}, {TokenWord, "/project"},
	}
	err := builtinHistory(args, &buf)
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

func TestBuiltinHistoryBadArgs(t *testing.T) {
	_, cleanup := withTestHistory(t)
	defer cleanup()

	var buf bytes.Buffer

	cases := []struct {
		name string
		args []Token
	}{
		{"negative N", []Token{{TokenWord, "-5"}}},
		{"zero N", []Token{{TokenWord, "0"}}},
		{"non-numeric N", []Token{{TokenWord, "abc"}}},
		{"since no value", []Token{{TokenWord, "--since"}}},
		{"until no value", []Token{{TokenWord, "--until"}}},
		{"dir no value", []Token{{TokenWord, "--dir"}}},
		{"since bad date", []Token{{TokenWord, "--since"}, {TokenWord, "not-a-date"}}},
		{"unknown flag", []Token{{TokenWord, "--bogus"}}},
	}

	for _, c := range cases {
		buf.Reset()
		if err := builtinHistory(c.args, &buf); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}
