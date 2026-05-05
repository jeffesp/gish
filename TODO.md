# Gish Codebase Review

## Design Issues

### Pipeline errors from intermediate stages are lost

command.go:106-111 — All stage errors are collected, but only the last one is returned. If stage 0 fails (e.g., command not found) but stage 1 succeeds (or gets EOF and exits 0), the user never sees the error. The pipelines plan also specifies "kill all still-running stages on failure" which isn't implemented — a failed early stage just closes its pipe and the pipeline drains.

### Global jsVM singleton is fragile

scripting.go:16 — InitScripting replaces the global jsVM on every call. Any builtin registered via gish.register() captures the JS callback from the old runtime, but builtinJS uses the new runtime. In production this is called once, but it's a footgun for testing and future changes.


### gish.source requires absolute paths to the files

### Pipeline can deadlock on large output

command.go:108-111 — Stages are waited sequentially (`waits[0]()`, then `waits[1]()`, etc). If an early stage produces more output than the OS pipe buffer (~64KB), it blocks on write. But its `wait()` won't return until the write completes, and the next stage (the reader) hasn't been waited yet. In practice this is masked because `Start` launches goroutines, but if a builtin is used as an early pipeline stage, it runs in a goroutine that could block. The real risk: if the builtin's goroutine holds `jsmu` while blocked on the pipe, the shell deadlocks.

### Pipe read-side is never explicitly closed

command.go:92-104 — The `pr` (read end of the pipe) created for each intermediate stage is passed as the next stage's stdin but never closed. The downstream process's exit closes its stdin handle, so in practice the fd is reclaimed, but it leaks if a stage fails before reading. Explicit `defer pr.Close()` after the stage that reads from it would be defensive.

### loadInitScript defers os.Chdir with a potentially-invalid path

scripting.go:354-355 — `currentDir` is used in a `defer os.Chdir(currentDir)` but the `err` from `os.Getwd()` is only printed, not returned. If `Getwd` fails (deleted cwd), the deferred Chdir will silently fail, leaving the shell in the config dir.

### No escape character support in tokenizer

token.go — There's no handling of backslash escapes (`\"`, `\ `, `\\`). This means you can't include a literal quote inside a same-type quoted string, and you can't escape spaces in unquoted words. This is a significant usability gap for a shell.

### Terminal title escape injection

builtins.go:108 / scripting.go:169 — The `title` builtin and `gish.title()` write user-supplied text directly into an OSC escape sequence without sanitizing control characters. A string containing `\007` or other escape sequences could inject arbitrary terminal control codes.

### termHistory.At has an off-by-one boundary check

history.go:159 — The guard is `if len(h.entries) < idx` but should be `<=`. When `idx == len(h.entries)`, the expression `len(h.entries)-1-idx` evaluates to `-1`, causing a panic on slice access.

### gish.register callbacks use ctx.Out from registration time, not invocation time

scripting.go:36-61 — `gish.print`/`gish.println` are closed over the `ctx` passed to `setupAPI`. If you register a command, then later pipe it (e.g., `mycommand | grep x`), the callback's `gish.println` still writes to the original terminal writer, not the pipe. The command itself receives a `bCtx` with the correct pipe, but the gish.print functions ignore it.

### Tests mutate global state without cleanup guards

scripting_test.go — Tests call `InitScripting` which replaces the global `jsVM`, `promptFn`, and mutates the `Builtins` map. Tests clean up individual registered builtins but don't restore the original VM/prompt state. This makes `t.Parallel()` unsafe and means test ordering can matter.

## Missing Features (acknowledged in README/plans)

### No $? / last exit code

There's no way to access the previous command's exit code from the shell or from JS scripts. The history records it, but it's not exposed as an env var or gish.lastExitCode.

### No ~ in cd

Probably need to support it not only for `cd`, but that is where I have noticed it most.

### Prompt function can fail

You don't get any output about what went wrong. There is an error swallowed by the execution of the JS, and it needs to be bubbled back to the `JSPrompt` function, I think.

### No multi-line input / line continuation

The REPL has no way to continue a command across multiple lines (e.g., trailing `\` or unclosed quotes prompting for more input). Every `ReadLine`/`Scan` is treated as complete.

### No command separators (`;`, `&&`, `||`)

The tokenizer and parser only handle pipes. There's no way to run `mkdir foo && cd foo` or `cmd1; cmd2` in a single line.

### expandHistory re-reads the history file on every invocation

history.go:169-193 — Every `!!` or `!prefix` calls `loadHistory()`, which opens and parses the entire history file. For a long-running session with large history, this is needlessly expensive. The in-memory `termHistory` exists but isn't used for expansion.

## Test Gaps

- signals.go — Completely untested. Signal forwarding to child processes is critical correctness code.
- Pipeline error paths — Only the happy path (echo | cat) is tested. Missing: failed stage, command-not-found in a stage, pipe creation failure.
- exec builtin — Understandably hard to test (syscall.Exec replaces the process), but could be tested up to the LookPath call.
- builtinCd — No test for `cd -` or `cd` with no args (home directory). OLDPWD behavior is uncovered.
- Alias recursion — The loop-prevention logic is tested implicitly via the alias expansion tests, but there's no explicit test that deeply-nested or mutually-recursive aliases terminate.
- Glob expansion — `expandGlobs` has no tests. The dot-file exclusion logic and error-path (glob returns error) are uncovered.
