package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type HistoryEntry struct {
	Command   string    `json:"cmd"`
	Dir       string    `json:"dir"`
	ExitCode  int       `json:"exit"`
	StartTime time.Time `json:"start"`
	EndTime   time.Time `json:"end"`
	SessionID string    `json:"session"`
}

var (
	historyFile string
	historyMax  int // 0 = unlimited
	sessionID   string
)

func init() {
	sessionID = fmt.Sprintf("%d", os.Getpid())

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	historyFile = filepath.Join(home, ".gish_history")

	if v := os.Getenv("GISH_HISTORY_FILE"); v != "" {
		historyFile = v
	}

	if v := os.Getenv("GISH_HISTORY_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			historyMax = n
		}
	}

	truncateHistory()

	RegisterBuiltin("history", builtinHistory)
}

func truncateHistory() {
	if historyMax == 0 {
		return
	}
	entries, err := loadHistory()
	if err != nil || len(entries) <= historyMax {
		return
	}
	entries = entries[len(entries)-historyMax:]
	writeHistory(entries)
}

func appendHistory(e HistoryEntry) {
	f, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	f.Write(data)
	f.Write([]byte("\n"))
}

func loadHistory() ([]HistoryEntry, error) {
	f, err := os.Open(historyFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e HistoryEntry
		if err := json.Unmarshal([]byte(line), &e); err == nil {
			entries = append(entries, e)
		}
	}
	return entries, scanner.Err()
}

func writeHistory(entries []HistoryEntry) {
	f, err := os.OpenFile(historyFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		f.Write(data)
		f.Write([]byte("\n"))
	}
}

const termHistorySize = 100

// termHistory implements the term.History interface backed by in-memory entries
// seeded from the history file. New entries are added in-memory only;
// file persistence happens in execLine via appendHistory.
type termHistory struct {
	entries []string
	max     int
}

func newTermHistory() *termHistory {
	h := &termHistory{max: termHistorySize}
	if entries, err := loadHistory(); err == nil {
		start := 0
		if len(entries) > h.max {
			start = len(entries) - h.max
		}
		for _, e := range entries[start:] {
			h.entries = append(h.entries, e.Command)
		}
	}
	return h
}

func (h *termHistory) Add(entry string) {
	h.entries = append(h.entries, entry)
	if len(h.entries) > h.max {
		h.entries = h.entries[len(h.entries)-h.max:]
	}
}

func (h *termHistory) Len() int {
	return len(h.entries)
}

// At returns the entry at index idx, where 0 is the most recent.
// Out-of-range indices return the empty string.
func (h *termHistory) At(idx int) string {
	if idx < 0 || idx >= len(h.entries) {
		return ""
	}
	return h.entries[len(h.entries)-1-idx]
}

// expandHistory handles !! (last command) and !prefix (most recent match).
// Returns the expanded line and true, or the original line and false.
func expandHistory(line string) (string, bool) {
	if line == "!!" {
		entries, err := loadHistory()
		if err != nil || len(entries) == 0 {
			return line, false
		}
		return entries[len(entries)-1].Command, true
	}
	if strings.HasPrefix(line, "!") && len(line) > 1 && line[1] != ' ' {
		prefix := line[1:]
		entries, err := loadHistory()
		if err != nil {
			return line, false
		}

		commandNum, err := strconv.Atoi(prefix)
		commandNum = commandNum - 1
		if err == nil && commandNum >= 0 && commandNum < len(entries) {
			return entries[commandNum].Command, true
		}

		for i := len(entries) - 1; i >= 0; i-- {
			if strings.HasPrefix(entries[i].Command, prefix) {
				return entries[i].Command, true
			}
		}
		return line, false
	}
	return line, false
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

const historyUsage = `usage: history [N] [--all] [--long] [--since DATE] [--until DATE] [--ok | --fail] [--dir PATH]

Show command history for the current session as "number  exit-code  command".
The number matches !N expansion.

  N              show only the last N entries (after other filters)
  --all          include entries from all sessions
  --long         also show start time, directory and duration
  --since DATE   only entries started on or after DATE (YYYY-MM-DD)
  --until DATE   only entries started on or before DATE (YYYY-MM-DD, inclusive)
  --ok           only entries that exited 0
  --fail         only entries that exited non-zero (exclusive with --ok)
  --dir PATH     only entries whose directory contains PATH
  -h, --help     show this help`

func builtinHistory(cmd *Command, ctx *ExecCtx) error {
	entries, err := loadHistory()
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}

	args := cmd.Args()

	var (
		limit     int
		since     time.Time
		until     time.Time
		onlyOK    bool
		onlyFail  bool
		dirFilter string
		hasSince  bool
		hasUntil  bool
		all       bool
		long      bool
	)

	for i := 0; i < len(args); i++ {
		v := args[i].Value
		switch v {
		case "--since":
			i++
			if i >= len(args) {
				return fmt.Errorf("--since requires a date (YYYY-MM-DD)")
			}
			t, err := time.ParseInLocation("2006-01-02", args[i].Value, time.Local)
			if err != nil {
				return fmt.Errorf("--since: invalid date %q (expected YYYY-MM-DD)", args[i].Value)
			}
			since = t
			hasSince = true
		case "--until":
			i++
			if i >= len(args) {
				return fmt.Errorf("--until requires a date (YYYY-MM-DD)")
			}
			t, err := time.ParseInLocation("2006-01-02", args[i].Value, time.Local)
			if err != nil {
				return fmt.Errorf("--until: invalid date %q (expected YYYY-MM-DD)", args[i].Value)
			}
			// end of that day
			until = t.Add(24*time.Hour - time.Nanosecond)
			hasUntil = true
		case "--ok":
			if onlyFail {
				return fmt.Errorf("--ok and --fail are mutually exclusive")
			}
			onlyOK = true
		case "--fail":
			if onlyOK {
				return fmt.Errorf("--ok and --fail are mutually exclusive")
			}
			onlyFail = true
		case "-h", "--help":
			fmt.Fprintln(ctx.Out, historyUsage)
			return nil
		case "--all":
			all = true
		case "--long":
			long = true
		case "--dir":
			i++
			if i >= len(args) {
				return fmt.Errorf("--dir requires a path")
			}
			dirFilter = args[i].Value
		default:
			if limit != 0 || strings.HasPrefix(v, "-") {
				return fmt.Errorf("unknown flag: %s\n%s", v, historyUsage)
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid count: %s\n%s", v, historyUsage)
			}
			limit = n
		}
	}

	// numbered keeps each entry's position in the full history so the
	// number shown matches what !N expands.
	type numbered struct {
		n int
		HistoryEntry
	}
	var filtered []numbered
	for idx, e := range entries {
		if !all && e.SessionID != sessionID {
			continue
		}
		if hasSince && e.StartTime.Before(since) {
			continue
		}
		if hasUntil && e.StartTime.After(until) {
			continue
		}
		if onlyOK && e.ExitCode != 0 {
			continue
		}
		if onlyFail && e.ExitCode == 0 {
			continue
		}
		if dirFilter != "" && !strings.Contains(e.Dir, dirFilter) {
			continue
		}
		filtered = append(filtered, numbered{idx + 1, e})
	}

	if limit > 0 && limit < len(filtered) {
		filtered = filtered[len(filtered)-limit:]
	}

	for _, e := range filtered {
		if !long {
			fmt.Fprintf(ctx.Out, "%5d  %3d  %s\n", e.n, e.ExitCode, e.Command)
			continue
		}
		dur := e.EndTime.Sub(e.StartTime).Round(time.Millisecond)
		fmt.Fprintf(ctx.Out, "%5d  %s  [%s] exit=%d dur=%v  %s\n",
			e.n,
			e.StartTime.Format("2006-01-02 15:04:05"),
			e.Dir,
			e.ExitCode,
			dur,
			e.Command,
		)
	}
	return nil
}
