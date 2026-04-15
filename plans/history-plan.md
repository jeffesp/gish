# Plan: Persistent Command History

## Context
gish currently has no history at all — every session is ephemeral. This adds persistent, richly-annotated history: entries are stored between runs with metadata (dir, exit code, timing, session ID), configurable truncation, and a `history` builtin to query them.

## Storage Format: NDJSON (`~/.gish_history`)

One JSON object per line. Appendable without rewriting the file (crash-safe). Human-readable with `jq`. Malformed/truncated lines at the end are silently skipped on load.

File location follows `~/.bash_history` / `~/.zsh_history` convention. Override with `GISH_HISTORY_FILE` env var (useful in tests).

## HistoryEntry Struct

```go
type HistoryEntry struct {
    Command   string    `json:"cmd"`
    Dir       string    `json:"dir"`
    ExitCode  int       `json:"exit"`
    StartTime time.Time `json:"start"`
    EndTime   time.Time `json:"end"`
    SessionID string    `json:"session"`
}
```

- `Dir` — cwd captured *before* execution (so `cd foo` records where you were)
- `ExitCode` — numeric; 0 success, 1 for builtin errors, `ExitCode()` for external commands, -1 for signal-killed
- `SessionID` — `fmt.Sprintf("%d", os.Getpid())`, no new deps needed
- Duration is derivable as `EndTime.Sub(StartTime)`, not stored separately

## Config

Consistent with existing `GISH_GIT_STATUS` env-var pattern:
- `GISH_HISTORY_MAX` — max entries to keep; 0 or unset = unlimited
- `GISH_HISTORY_FILE` — override path (default `~/.gish_history`)

Truncation runs **once at startup** (read → slice to last N → rewrite), before any appends from the new session. Append-per-command is crash-safe; no mid-session rewriting.

## Files to Change

### `history.go` (new)
All history logic lives here. Registered via its own `init()`.

Functions:
- `init()` — sets `sessionID`, resolves `historyFile`, parses `GISH_HISTORY_MAX`, calls `truncateHistory()`, registers `history` builtin
- `appendHistory(HistoryEntry)` — opens `O_APPEND|O_CREATE`, marshals entry, writes line; **silent on error** (history is best-effort)
- `loadHistory() ([]HistoryEntry, error)` — scans NDJSON, skips malformed lines
- `writeHistory([]HistoryEntry)` — truncates and rewrites file
- `truncateHistory()` — no-op if `historyMax == 0`; otherwise loads, slices, rewrites
- `exitCode(err error) int` — extracts numeric code via `errors.As(err, &*exec.ExitError)`; returns 0/1/-1 as appropriate
- `builtinHistory(args []Token, w io.Writer) error` — `history` (all) or `history N` (last N)

Imports: all stdlib — `bufio`, `encoding/json`, `errors`, `fmt`, `io`, `os`, `os/exec`, `path/filepath`, `strconv`, `strings`, `time`

### `repl.go`
Add `"time"` to imports. Modify `execLine` to:
1. Capture `dir, _ := os.Getwd()` and `start := time.Now()` before execution
2. Restructure builtin/external as explicit `if/else` (was two separate `return`-guarded blocks) to share the `appendHistory` call at the end
3. Capture `code` from both paths (builtin error → 1, external → `exitCode(err)`)
4. Call `appendHistory(HistoryEntry{...})` after execution

```go
func execLine(line string, out io.Writer, errOut io.Writer, runCmd func(*exec.Cmd) error) {
    tokens, err := tokenize(line)
    if err != nil {
        fmt.Fprintf(errOut, "error: %v\n", err)
        return
    }

    dir, _ := os.Getwd()
    start := time.Now()
    code := 0

    name, args := tokens[0].Value, tokens[1:]
    if fn, ok := Builtins[name]; ok {
        if err := fn(args, out); err != nil {
            fmt.Fprintf(errOut, "error: %v\n", err)
            code = 1
        }
    } else {
        cmd := exec.Command(name, tokenValues(args)...)
        SetCurrentCmd(cmd)
        if err := runCmd(cmd); err != nil {
            fmt.Fprintf(errOut, "error: %v\n", err)
            code = exitCode(err)
        }
        ClearCurrentCmd()
    }

    appendHistory(HistoryEntry{
        Command:   line,
        Dir:       dir,
        ExitCode:  code,
        StartTime: start,
        EndTime:   time.Now(),
        SessionID: sessionID,
    })
}
```

### `builtins.go`, `main.go`, `signals.go`
No changes needed.

## `history` Builtin Interface

```
history       — show all entries
history N     — show last N entries
```

Output format:
```
    1  2026-04-14 09:12:03  [/Users/jeff/Code/gish] exit=0 dur=234ms  ls -la
    2  2026-04-14 09:12:11  [/Users/jeff/Code/gish] exit=1 dur=12ms   cat nonexistent
```

## Verification

Add `history_test.go` covering:
- `appendHistory` + `loadHistory` round-trip
- `truncateHistory` respects `GISH_HISTORY_MAX`
- `builtinHistory` with no args, with `N`, with bad args
- `exitCode` helper for nil / `*exec.ExitError` / other errors
- Tests use `GISH_HISTORY_FILE=t.TempDir()+"/hist"` to avoid touching `~/.gish_history`

Run: `go test -v ./...`
