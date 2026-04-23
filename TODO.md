  Gish Codebase Review

  Overall: clean, well-structured code with solid test coverage for a ~1500 LOC shell. The issues below are ordered by severity.

  Bugs

  1. cmd.Start() error silently discarded in pipelines

  command.go:46 — Start() returns an error that's thrown away. If the binary doesn't exist, cmd.Wait() will return a confusing error instead of "executable not found."

  // current
  cmd.Start()

  // should be
  if err := cmd.Start(); err != nil {
      clearCmd()
      return func() error { return err }
  }

  2. termHistory.At() will panic on out-of-bounds

  history.go:159 — No bounds check. If term.Terminal ever calls At(idx) with idx >= Len(), you get an index-out-of-range panic. Defensive guard is cheap.

  3. tokenize doesn't flush pending word before a pipe

  token.go:68-69 — When hitting |, the current cur builder is not flushed first. Input like echo hello|cat produces tokens [{Word, "echohello"}, {Pipe, "|"}, ...] — the word before the pipe gets concatenated with
  whatever preceded it, and the pipe eats the adjacent character. Whitespace around pipes (echo hello | cat) works, but bare pipes adjacent to words do not.

  4. Error message has leading space

  repl.go:54 — fmt.Fprintf(ctx.ErrOut, " %v\n", err) has a stray leading space, inconsistent with lines 38 and 45.

  Design Issues

  5. Pipeline errors from intermediate stages are lost

  command.go:106-111 — All stage errors are collected, but only the last one is returned. If stage 0 fails (e.g., command not found) but stage 1 succeeds (or gets EOF and exits 0), the user never sees the error.
  The pipelines plan also specifies "kill all still-running stages on failure" which isn't implemented — a failed early stage just closes its pipe and the pipeline drains.

  6. expandHistory reads the full history file on every !! / !prefix

  history.go:164-186 — loadHistory() reads and parses the entire NDJSON file each time. During normal use this is fine, but as the history grows it becomes O(n) per command for a feature most people use rarely. A
  simple cache (or reading from termHistory.entries which is already in memory) would fix this.

  7. Global jsVM singleton is fragile

  scripting.go:16 — InitScripting replaces the global jsVM on every call. Any builtin registered via gish.register() captures the JS callback from the old runtime, but builtinJS uses the new runtime. In production
   this is called once, but it's a footgun for testing and future changes.

  8. RunCmd vs RestoreTerm — overlapping terminal management

  context.go:12-13 — Two callbacks on ExecCtx both manage raw/cooked terminal transitions. RunCmd wraps a single command's Run(), RestoreTerm is called before a pipeline. This split works but creates an implicit
  contract: single commands use one path, pipelines use another. A unified approach would be clearer.

  Missing Features (acknowledged in README/plans)

  9. No exit builtin

  The shell can only be exited via Ctrl-D (EOF) or a signal. An exit [code] command is table-stakes for a shell.

  10. No glob expansion

  ls *.go passes the literal *.go to ls. On macOS/Linux, external commands don't expand globs — that's the shell's job. The README acknowledges this is coming.

  11. No cd - (previous directory)

  Noted as a TODO in builtins.go:60.

  12. No $? / last exit code

  There's no way to access the previous command's exit code from the shell or from JS scripts. The history records it, but it's not exposed as an env var or gish.lastExitCode.

  Minor / Cosmetic

  13. go.mod says go 1.26.2

  This Go version doesn't exist. Should match whatever you're actually building with (likely 1.22+, since you use for range N).

  14. parseTokens comment typo

  token.go:105 — "make sure pipe are in valid places" should be "pipes are".

  15. Unused import potential in builtins.go

  exec is imported only for exec.LookPath in builtinExec. Not a problem, just noting the coupling.

  Test Gaps

  - signals.go — Completely untested. Signal forwarding to child processes is critical correctness code.
  - Pipeline error paths — Only the happy path (echo | cat) is tested. Missing: failed stage, command-not-found in a stage, pipe creation failure.
  - tokenize with adjacent pipes — foo|bar behavior isn't tested (and is buggy per item 3).
  - exec builtin — Understandably hard to test (syscall.Exec replaces the process), but could be tested up to the LookPath call.

  What's Done Well

  - Clean separation of concerns across files
  - Executable interface with Command/Pipeline is a good abstraction
  - History system is thorough — NDJSON, rich filtering, terminal integration
  - JS scripting API is well-designed with exec/spawn/register/env
  - callSafe properly handles goja's panic-based error model
  - Test coverage for history and scripting is strong
  - Plans-driven development keeps the codebase coherent

  The highest-priority fixes are items 1 (discarded Start error) and 3 (tokenizer pipe-adjacent bug), as both produce incorrect behavior in normal use. Item 5 (pipeline error propagation) is worth addressing
  before building out the Phase 2 JSON pipes.
