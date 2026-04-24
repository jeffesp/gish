Gish Codebase Review

Overall: clean, well-structured code with solid test coverage for a ~1500 LOC shell. The issues below are ordered by severity.

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

10. No glob expansion

ls _.go passes the literal _.go to ls. On macOS/Linux, external commands don't expand globs — that's the shell's job. The README acknowledges this is coming.

11. No cd - (previous directory)

Noted as a TODO in builtins.go:60.

12. No $? / last exit code

There's no way to access the previous command's exit code from the shell or from JS scripts. The history records it, but it's not exposed as an env var or gish.lastExitCode.

Minor / Cosmetic

15. Unused import potential in builtins.go

exec is imported only for exec.LookPath in builtinExec. Not a problem, just noting the coupling.

Test Gaps

- signals.go — Completely untested. Signal forwarding to child processes is critical correctness code.
- Pipeline error paths — Only the happy path (echo | cat) is tested. Missing: failed stage, command-not-found in a stage, pipe creation failure.
- tokenize with adjacent pipes — foo|bar behavior isn't tested (and is buggy per item 3).
- exec builtin — Understandably hard to test (syscall.Exec replaces the process), but could be tested up to the LookPath call.
