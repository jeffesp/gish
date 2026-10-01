# Gish Codebase Review

## Design Issues

### Global jsVM singleton is fragile

scripting.go:16 — InitScripting replaces the global jsVM on every call. Any builtin registered via gish.register() captures the JS callback from the old runtime, but builtinJS uses the new runtime. In production this is called once, but it's a footgun for testing and future changes.


### gish.source requires absolute paths to the files

### Terminal title escape injection

builtins.go:108 / scripting.go:169 — The `title` builtin and `gish.title()` write user-supplied text directly into an OSC escape sequence without sanitizing control characters. A string containing `\007` or other escape sequences could inject arbitrary terminal control codes.

### gish.register callbacks use ctx.Out from registration time, not invocation time

scripting.go:36-61 — `gish.print`/`gish.println` are closed over the `ctx` passed to `setupAPI`. If you register a command, then later pipe it (e.g., `mycommand | grep x`), the callback's `gish.println` still writes to the original terminal writer, not the pipe. The command itself receives a `bCtx` with the correct pipe, but the gish.print functions ignore it.

### ~~Completion's Ctrl+C/Enter handling works by pre-filtering raw bytes~~ — resolved by forking x/term's terminal.go

`internal/term` now carries gish's own copy of x/term's `terminal.go` (see its package doc comment). `ctrlCFilter` (ctrlc.go) and `enterFilter` (complete.go) are gone: Ctrl+C is a real `case keyCtrlC` in `handleKey`'s switch now (fixing the old `t.remainder` bug as a side effect, since it no longer returns early from `readLine` and skips its bookkeeping), and a new `PreKeyCallback` field fires for every decoded key — including ones, like Enter, that `AutoCompleteCallback` never sees — so `complete.go` no longer needs to intercept raw bytes to clear a stale listing.

Completion also no longer guesses the cursor's screen column from `completionPrompt`'s length: `term.Terminal` exposes a `CursorColumn()` accessor now, and `printCandidatesBelow` reads the real position straight off it.

Still open: arrows, Home/End, Delete, and Alt+Left/Right are already decoded by `bytesToKey` (and were before the fork). What's actually still unhandled — PageUp/PageDown/Insert (`\x1b[5~`/`\x1b[6~`/`\x1b[2~`) and function keys — fall through to the generic ending-byte scan and collapse to `keyUnknown`. Straightforward to add now that we own the file — just not needed yet.

### Tests mutate global state without cleanup guards

scripting_test.go — Tests call `InitScripting` which replaces the global `jsVM`, `promptFn`, and mutates the `Builtins` map. Tests clean up individual registered builtins but don't restore the original VM/prompt state. This makes `t.Parallel()` unsafe and means test ordering can matter.

## Missing Features (acknowledged in README/plans)

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
- Pipeline error paths — Failed stage, command-not-found in a stage, and kill-on-failure are covered. Missing: pipe creation failure.
- exec builtin — Understandably hard to test (syscall.Exec replaces the process), but could be tested up to the LookPath call.
- builtinCd — No test for `cd -` or `cd` with no args (home directory). OLDPWD behavior is uncovered.
- Alias recursion — The loop-prevention logic is tested implicitly via the alias expansion tests, but there's no explicit test that deeply-nested or mutually-recursive aliases terminate.
- Glob expansion — `expandGlobs` has no tests. The dot-file exclusion logic and error-path (glob returns error) are uncovered.
