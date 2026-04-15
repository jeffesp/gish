# Plan: Persistent Command History

## Context
gish currently has no history at all — every session is ephemeral. This adds persistent, richly-annotated history: entries stored between runs with metadata (dir, exit code, timing, session ID), configurable truncation, and a `history` builtin with flexible filtering.

## Storage Format: NDJSON (`~/.gish_history`)

One JSON object per line. Appended per command — crash-safe. Human-readable with `jq`. Malformed/truncated lines silently skipped on load.

File follows `~/.bash_history` / `~/.zsh_history` convention. Override with `GISH_HISTORY_FILE` (useful in tests).

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
- `ExitCode` — 0 success, 1 for builtin errors, `ExitCode()` for external cmds, -1 for signal-killed
- `SessionID` — `fmt.Sprintf("%d", os.Getpid())`, no new deps
- Duration derivable as `EndTime.Sub(StartTime)`, not stored separately

## Config (env vars, consistent with `GISH_GIT_STATUS` pattern)

- `GISH_HISTORY_MAX` — max entries to retain; 0 or unset = unlimited
- `GISH_HISTORY_FILE` — override path (default `~/.gish_history`)

Truncation runs **once at startup**: load → slice to last N → rewrite if needed. Append-per-command is crash-safe; no mid-session rewriting.

## Files to Change

### `history.go` (new)

All history logic. Registered via its own `init()`.

Functions:
- `init()` — sets `sessionID`, resolves `historyFile`, parses `GISH_HISTORY_MAX`, calls `truncateHistory()`, registers `history` builtin
- `appendHistory(HistoryEntry)` — `O_APPEND|O_CREATE`, marshal + write line; **silent on error** (best-effort)
- `loadHistory() ([]HistoryEntry, error)` — scans NDJSON, skips malformed lines; loads all into slice
- `writeHistory([]HistoryEntry)` — truncates and rewrites file
- `truncateHistory()` — no-op if `historyMax == 0`; else load → slice → rewrite
- `exitCode(err error) int` — `errors.As(err, &*exec.ExitError)` → `.ExitCode()`; 0/1/-1
- `builtinHistory(args []Token, w io.Writer) error` — see interface below

Imports: all stdlib — `bufio`, `encoding/json`, `errors`, `fmt`, `io`, `os`, `os/exec`, `path/filepath`, `strconv`, `strings`, `time`

### `repl.go`

Add `"time"` to imports. Modify `execLine`:
1. Capture `dir, _ := os.Getwd()` and `start := time.Now()` before execution
2. Restructure builtin/external as `if/else` to share `appendHistory` call at end
3. Capture numeric `code` from both paths
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

### `builtins.go`, `main.go`, `signals.go` — no changes

## `history` Builtin Interface

All flags are optional and combinable.

```
history [N] [--since DATE] [--until DATE] [--ok | --fail] [--dir PATH]
```

| Flag | Meaning |
|---|---|
| `N` | Show last N results (after all other filters applied) |
| `--since DATE` | Only entries with StartTime >= DATE |
| `--until DATE` | Only entries with StartTime <= DATE |
| `--ok` | Only entries with ExitCode == 0 |
| `--fail` | Only entries with ExitCode != 0 |
| `--dir PATH` | Only entries where Dir contains PATH as a substring |

`DATE` format: `YYYY-MM-DD` (parsed as local midnight). `--ok` and `--fail` are mutually exclusive.

**Parsing approach:** walk `args` in order. If `args[0]` parses as a positive integer and is not a flag, treat as N. Remaining args are `--flag value` pairs. Return error on unknown flags or bad values.

**Execution:** load all entries into `[]HistoryEntry`, apply filters in order (since → until → ok/fail → dir), then take last N if specified.

**Output format:**
```
    1  2026-04-14 09:12:03  [/Users/jeff/Code/gish] exit=0 dur=234ms  ls -la
    2  2026-04-14 09:12:11  [/Users/jeff/Code/gish] exit=1 dur=12ms   cat nonexistent
```

## Verification

Add `history_test.go` covering:
- `appendHistory` + `loadHistory` round-trip
- `truncateHistory` respects `GISH_HISTORY_MAX`
- `builtinHistory`: no args, N, `--since`, `--until`, `--ok`, `--fail`, `--dir`, combined flags, bad args
- `exitCode` for nil / `*exec.ExitError` / other errors
- All tests use `os.Setenv("GISH_HISTORY_FILE", t.TempDir()+"/hist")` to avoid touching `~/.gish_history`

Run: `go test -v ./...`
