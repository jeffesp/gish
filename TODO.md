# Gish Codebase Review

Overall: clean, well-structured code with solid test coverage for a ~1500 LOC shell. The issues below are ordered by severity.

## Design Issues

### Pipeline errors from intermediate stages are lost

command.go:106-111 — All stage errors are collected, but only the last one is returned. If stage 0 fails (e.g., command not found) but stage 1 succeeds (or gets EOF and exits 0), the user never sees the error. The pipelines plan also specifies "kill all still-running stages on failure" which isn't implemented — a failed early stage just closes its pipe and the pipeline drains.

### Global jsVM singleton is fragile

scripting.go:16 — InitScripting replaces the global jsVM on every call. Any builtin registered via gish.register() captures the JS callback from the old runtime, but builtinJS uses the new runtime. In production this is called once, but it's a footgun for testing and future changes.

### RunCmd vs RestoreTerm — overlapping terminal management

context.go:12-13 — Two callbacks on ExecCtx both manage raw/cooked terminal transitions. RunCmd wraps a single command's Run(), RestoreTerm is called before a pipeline. This split works but creates an implicit
contract: single commands use one path, pipelines use another. A unified approach would be clearer.

## Missing Features (acknowledged in README/plans)

### No $? / last exit code

There's no way to access the previous command's exit code from the shell or from JS scripts. The history records it, but it's not exposed as an env var or gish.lastExitCode.

### No way to alias commands

What I want is to have a builtin that sets a permanent alias and then maybe some commands to manage. Or it can just write to a alias.js that is in the config dir.

## Test Gaps

- signals.go — Completely untested. Signal forwarding to child processes is critical correctness code.
- Pipeline error paths — Only the happy path (echo | cat) is tested. Missing: failed stage, command-not-found in a stage, pipe creation failure.
- tokenize with adjacent pipes — foo|bar behavior isn't tested (and is buggy per item 3).
- exec builtin — Understandably hard to test (syscall.Exec replaces the process), but could be tested up to the LookPath call.
