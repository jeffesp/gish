# JS Error Trace Files

## Context
JS errors in gish today are reduced to just the message: every call site does
`ex.Value().String()` on the `*goja.Exception`, which throws away the stack
(`builtinJS`, `builtinSource`, `callSafe` in `scripting.go`). Errors in the
prompt function are dropped completely (`JSPrompt` falls back to `gish> `
without saying anything). That makes init.js and prompt.js hard to debug. The goal is to
write the full goja stack to its own file each time a JS error happens, delete old
files at startup, and let the user turn it off with an env var.

## Behavior
- Directory: `$GISH_TRACE_DIR`, else `$XDG_STATE_HOME/gish/traces`, else
  `~/.local/state/gish/traces`. Created with `MkdirAll(dir, 0700)` the first time a trace is written.
- One file per trace: `<YYYYMMDD-HHMMSS.000>-<pid>-<seq>.txt`, mode 0600. The pid
  and an atomic sequence number keep two shells, or two errors in the same millisecond,
  from overwriting each other's file.
- Contents: timestamp, gish `Version`, pid, cwd, the origin (e.g. `js`, `source foo.js`,
  `init.js`, `prompt`, `register:<name>`, `spawn callback`), then `ex.String()`.
  goja's `Exception.String()` holds the message plus the `at …` stack frames. A
  non-Exception error, such as a compile or syntax error, is written with `%v`.
- Retention: `GISH_KEEP_TRACE_DAYS` (default 7). At startup a goroutine deletes the
  `*.txt` files in the trace dir whose mtime is older than N days. N ≤ 0 means keep
  forever. A value that doesn't parse falls back to the default.
- Disable: `GISH_TRACES=0` (also `off` or `false`) means no writes and no cleanup goroutine.
- User-facing errors get ` (trace: <path>)` appended when a trace was written, e.g.
  `gish: js: Error: boom (trace: ~/.local/state/gish/traces/…txt)`. This applies to
  errors returned from `js`, `source` and `register` builtins, and to the init.js
  messages on stderr. The prompt has no error output today. When a prompt trace is
  written, print one line to `ErrOut`, `gish: prompt error (trace: <path>)`, only when
  the error changes (see the dedupe note below).
- Directory creation: when tracing is enabled, `writeJSTrace` calls
  `os.MkdirAll(dir, 0700)` before every write. This covers a missing directory, a
  missing `~/.local/state`, and a directory deleted while gish is running. If
  MkdirAll fails, no file is written and no path is appended. Cleanup on a directory
  that doesn't exist is a quiet no-op, and cleanup never creates the directory.
- Every write is best-effort. A write failure is silent and never changes the error the user sees.

## Changes

### New file `trace.go`
- `traceDir() string`: resolves the directory, following the pattern of `configDir()` (`scripting.go:359`).
- `tracesEnabled() bool`: reads `GISH_TRACES`.
- `writeJSTrace(origin string, err error) string`: returns nothing if tracing is
  disabled or `err` is nil. Otherwise it builds the body, writes the file and returns the path ("" on
  failure). It unwraps with `errors.As(err, &*goja.Exception)` to get the stack. For
  `gish.source` nested inside JS, the exception is wrapped in a GoError, so `errors.As`
  still finds the inner exception.
- `cleanupTraces(dir string, maxAge time.Duration)`: `os.ReadDir`, then removes old `.txt` files. Errors are ignored.
- `startTraceCleanup()`: reads `GISH_KEEP_TRACE_DAYS`, then `go cleanupTraces(...)`.

### `scripting.go`
- `InitScripting` (line 22): call `startTraceCleanup()` once. Use a `sync.Once`,
  because tests call `InitScripting` many times.
- `callSafe` (line 418): it flattens the exception to `fmt.Errorf(ex.Value().String())`,
  so the stack is lost before callers see it. Add an `origin string` parameter and call
  `writeJSTrace(origin, ...)` on both the recovered panic path and the returned-error
  path (goja returns `*Exception` as `err` from `fn(...)`, and that path is not flattened now). Update the
  three callers:
  - `gish.register` callback (line 49): origin `"register:"+name`
  - `gish.spawn` callback (line 130): not traced on its own. The raw exception is
    re-thrown into the calling JS, and the top-level trace includes its stack
    through `Exception.Unwrap`.
  - `JSPrompt` (line 282): origin `"prompt"`. This is the important one, since it is silent today.
- `builtinJS` (line 310) and `builtinSource` (line 347): call `writeJSTrace` before
  converting the error. Do it after `jsmu.Unlock()` so no file I/O happens while the lock is held.
- `loadInitScript` (lines 398, 401): trace both the compile and run errors with origin `"init.js"`.
- Prompt errors run on every keypress-redraw/prompt: a broken prompt fn would write
  one file per prompt. Mitigation: in `JSPrompt`, trace only when the error string
  differs from the last traced prompt error, using a package var.

### `main_test.go`
- In `TestMain`, set `os.Setenv("GISH_TRACES", "0")` so the suite never writes
  traces. Also point `GISH_TRACE_DIR` at `filepath.Join(tmp, "traces")` as a
  second guard, so nothing can ever land in the real `~/.local/state`.
- Existing tests that compare error strings exactly are unaffected, because no
  `(trace: …)` suffix is added while tracing is disabled.

### `trace_test.go` (new)
These are the only tests that turn tracing back on. Each one uses
`t.Setenv("GISH_TRACES", "1")` and `t.Setenv("GISH_TRACE_DIR", filepath.Join(t.TempDir(), "a", "b"))`.
The nested path does not exist yet, so the tests also exercise directory creation.
- The trace dir is created when it is missing. The returned error contains `(trace: <path>)` and that file exists.
- `traceDir` precedence: GISH_TRACE_DIR > XDG_STATE_HOME > home. Same style as the
  configDir test at `scripting_test.go:658`.
- `js throw new Error("boom")` through `builtinJS` writes exactly one file, and that file contains `boom` and `at `.
- A failing `gish.register` callback and a failing prompt fn each write a trace. A
  repeated prompt error writes only one file.
- `GISH_TRACES=0` writes no files.
- `cleanupTraces`: create files, backdate some with `os.Chtimes`, run the cleanup, and check that only the new ones remain.

### `README.md`
- In the scripting section (around line 96), document `GISH_TRACE_DIR`,
  `GISH_KEEP_TRACE_DAYS` and `GISH_TRACES`, next to the other env vars.

## Verification
- `go test ./...` and `go vet ./...`.
- Manual check: `GISH_TRACE_DIR=$(mktemp -d) go run .`, then run
  `js function f(){throw new Error("x")} f()`. Check that a file appears with the `at f` frame.
  Put a throwing `setPrompt` in a temp `GISH_CONFIG_DIR/init.js` and confirm it writes one trace, not one per prompt.
- Backdate a file with `touch -d '10 days ago'`, restart gish and confirm the file is removed.
