# Consolidate RunCmd and RestoreTerm in ExecCtx

## Context

`ExecCtx` has two terminal-related callbacks (`RunCmd` and `RestoreTerm`) that both manage the same raw/cooked terminal toggle. `RunCmd` also bundles stdio wiring, which is duplicated in `Command.Start` and the nil-`RunCmd` fallback in `Command.Exec`. This is called out in `TODO.md` as a known issue.

The root cause is a two-layer IO problem in raw REPL mode: builtins write to `*term.Terminal` (handles `\r\n` in raw mode), while external commands must write to the raw OS file descriptors. `RunCmd` encodes this by closing over the outer context's IO handles.

## Approach: Remove RunCmd, add RawIO field + WireCmd method

Replace `RunCmd` with a `RawIO *ExecCtx` field pointing to raw IO handles for external commands, and a `WireCmd` helper method for stdio wiring. `RestoreTerm` becomes the single terminal management callback.

## Changes

### 1. context.go — Restructure ExecCtx

Remove `RunCmd`. Add `RawIO` and `WireCmd`:

```go
type ExecCtx struct {
    In          io.Reader
    Out         io.Writer
    ErrOut      io.Writer
    RawIO       *ExecCtx      // raw I/O for external commands; nil = use In/Out/ErrOut
    RestoreTerm func() func() // restore terminal; returns func to re-enter raw mode
}

func (ctx *ExecCtx) WireCmd(cmd *exec.Cmd) {
    src := ctx
    if ctx.RawIO != nil {
        src = ctx.RawIO
    }
    cmd.Stdin = src.In
    cmd.Stdout = src.Out
    cmd.Stderr = src.ErrOut
}
```

### 2. command.go — Command.Exec (lines 56-71)

Replace the `RunCmd` / fallback branch with `WireCmd` + `RestoreTerm`:

```go
ctx.WireCmd(cmd)
if ctx.RestoreTerm != nil {
    reenter := ctx.RestoreTerm()
    defer reenter()
}
return cmd.Run()
```

### 3. command.go — Command.Start (lines 43-45)

Replace 3-line stdio wiring with `ctx.WireCmd(cmd)`.

### 4. command.go — Pipeline.Exec (lines 77-81)

No change. Already uses `RestoreTerm` only.

### 5. repl.go — runRawREPL (lines 90-106)

Replace the 16-line `termCtx` with:

```go
termCtx := &ExecCtx{
    In:     in,
    Out:    t,
    ErrOut: t,
    RawIO:  ctx,  // ctx has In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr
    RestoreTerm: func() func() {
        term.Restore(fd, origState)
        return func() { term.MakeRaw(fd) } //nolint:errcheck
    },
}
```

`RawIO: ctx` gives external commands the raw OS handles. The 9-line `RunCmd` closure is gone.

### 6. repl.go — runScannerREPL (lines 141-147)

Delete the entire `ctx.RunCmd = ...` block. In scanner mode, `In/Out/ErrOut` are already raw OS handles; `RawIO` and `RestoreTerm` default to nil. Behavior is identical with zero setup.

Remove `"os/exec"` from repl.go imports.

### 7. builtins.go — builtinExec (lines 92-94)

No change. Already uses `RestoreTerm` only.

### 8. command_test.go

- **Replace** `TestCommandExecUsesRunCmd` with `TestCommandExecUsesRestoreTerm` — verify RestoreTerm is called and the re-enter function runs after execution.
- **Add** `TestCommandExecUsesRawIO` — verify RawIO handles are used when set.
- **Update** `TestPipelineRunsMultipleCommands` — remove `RunCmd` field from test context. RestoreTerm assertions unchanged.
- Remove `"os/exec"` import.

### 9. TODO.md / plans/*.md

Update references to `RunCmd` in `TODO.md` (remove the item) and note in `plans/redirection.md` that `RunCmd = nil` becomes `RawIO = nil`.

## Net impact

| Metric | Before | After |
|--------|--------|-------|
| Terminal toggle locations | 2 (RunCmd + RestoreTerm) | 1 (RestoreTerm only) |
| Stdio wiring locations | 3 (RunCmd, Exec fallback, Start) | 1 (WireCmd) |
| runRawREPL termCtx lines | 16 | 8 |
| runScannerREPL setup | 5 lines | 0 |
| New code | — | WireCmd (~7 lines) |

## Verification

1. `go build ./...` — compiles cleanly
2. `go test ./...` — all tests pass
3. Manual: run `gish`, execute an external command (e.g. `ls`), verify output is correct
4. Manual: run a pipeline (e.g. `ls | grep go`), verify output
5. Manual: run `exec bash`, verify terminal is sane
