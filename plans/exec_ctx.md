# Plan: Introduce ExecCtx struct

## Context
`out io.Writer`, `errOut io.Writer`, and `args []Token` are threaded through multiple levels. Adding new execution context (env overrides, last exit code, aliases) would mean touching every signature again. This introduces a single `*ExecCtx` struct to hold I/O, the parsed command line, and the raw input — providing a natural place for future context.

## The struct — `context.go` (new)

```go
type ExecCtx struct {
    In     io.Reader // input source (type-assert to *os.File where fd is needed)
    Line   string    // raw input line as typed
    Tokens []Token   // parsed tokens (first is command name)
    Out    io.Writer
    ErrOut io.Writer
}

// Args returns the argument tokens (everything after the command name).
func (ctx *ExecCtx) Args() []Token {
    if len(ctx.Tokens) < 2 {
        return nil
    }
    return ctx.Tokens[1:]
}

// Name returns the command name (first token value).
func (ctx *ExecCtx) Name() string {
    if len(ctx.Tokens) == 0 {
        return ""
    }
    return ctx.Tokens[0].Value
}
```

`Args()` and `Name()` are methods so builtins and execLine don't repeat the slice logic.

## BuiltinFunc signature change — `builtins.go`

```go
// before
type BuiltinFunc func(args []Token, w io.Writer) error

// after
type BuiltinFunc func(ctx *ExecCtx) error
```

Builtins get everything from `ctx`: args via `ctx.Args()`, output via `ctx.Out`. Cleaner — one parameter instead of two.

Each builtin: `args` → `ctx.Args()`, `args[i]` → `ctx.Args()[i]`, `w` → `ctx.Out`. Some builtins will capture `args := ctx.Args()` at the top for readability.

## execLine — `repl.go`

```go
// before
func execLine(line string, out io.Writer, errOut io.Writer, runCmd func(*exec.Cmd) error)

// after
func execLine(line string, ctx *ExecCtx, runCmd func(*exec.Cmd) error)
```

`execLine` tokenizes, populates `ctx.Line` and `ctx.Tokens`, then dispatches. The `line` parameter stays since tokenization happens inside `execLine` and it's what populates the ctx fields.

```go
func execLine(line string, ctx *ExecCtx, runCmd func(*exec.Cmd) error) {
    tokens, err := tokenize(line)
    if err != nil {
        fmt.Fprintf(ctx.ErrOut, "error: %v\n", err)
        return
    }
    ctx.Line = line
    ctx.Tokens = tokens

    dir, _ := os.Getwd()
    start := time.Now()
    code := 0

    if fn, ok := Builtins[ctx.Name()]; ok {
        if err := fn(ctx); err != nil {
            fmt.Fprintf(ctx.ErrOut, "error: %v\n", err)
            code = 1
        }
    } else {
        cmd := exec.Command(ctx.Name(), tokenValues(ctx.Args())...)
        SetCurrentCmd(cmd)
        if err := runCmd(cmd); err != nil {
            fmt.Fprintf(ctx.ErrOut, "error: %v\n", err)
            code = exitCode(err)
        }
        ClearCurrentCmd()
    }

    appendHistory(HistoryEntry{
        Command:   ctx.Line,
        Dir:       dir,
        ExitCode:  code,
        StartTime: start,
        EndTime:   time.Now(),
        SessionID: sessionID,
    })
}
```

## REPL functions — `repl.go`

```go
// before
func RunREPL(in io.Reader, out io.Writer, errOut io.Writer, afterCmd func(io.Writer, io.Writer))

// after
func RunREPL(ctx *ExecCtx, afterCmd func(*ExecCtx))
```

Same change for `runRawREPL` and `runScannerREPL` — `in` is now `ctx.In`.

`RunREPL` dispatches: `ctx.In.(*os.File)` + `term.IsTerminal` → `runRawREPL`, else `runScannerREPL`.

In `runRawREPL`, `in := ctx.In.(*os.File)` gets the fd. A local `termCtx := &ExecCtx{In: in, Out: t, ErrOut: t}` is created for builtin I/O through the terminal. The `runCmd` callback uses `ctx.Out`/`ctx.ErrOut` (raw writers) for `cmd.Stdout`/`cmd.Stderr`, and `ctx.In` for `cmd.Stdin`.

## main.go

```go
ctx := &ExecCtx{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
RunREPL(ctx, func(ctx *ExecCtx) {
    // afterCmd: out → ctx.Out, errOut → ctx.ErrOut
})
```

## history.go

Only `builtinHistory` changes: signature becomes `func builtinHistory(ctx *ExecCtx) error`, uses `ctx.Args()` and `ctx.Out`.

## signals.go, token.go, status.go — no changes

## Test changes

Add a helper:
```go
func testCtx(args []Token, out io.Writer) *ExecCtx {
    // Prepend a dummy command name so Args() works correctly
    tokens := append([]Token{{TokenWord, "test"}}, args...)
    return &ExecCtx{In: strings.NewReader(""), Tokens: tokens, Out: out, ErrOut: io.Discard}
}
```

All builtin test call sites: `fn(args, &buf)` → `fn(testCtx(args, &buf))`.

The existing `words()` helper stays — it builds `[]Token` which gets passed to `testCtx`.

`tok_test.go` — unchanged.

## Files modified
- `context.go` (new) — ExecCtx struct, Args(), Name()
- `builtins.go` — BuiltinFunc type + all 6 builtins
- `history.go` — builtinHistory only
- `repl.go` — execLine, RunREPL, runRawREPL, runScannerREPL
- `main.go` — construct ExecCtx, update afterCmd callback
- `builtins_test.go` — add testCtx helper, update all call sites
- `history_test.go` — update builtinHistory call sites

## What does NOT change
- tokenize, tokenValues, Token, TokenKind
- appendHistory, loadHistory, writeHistory, truncateHistory, exitCode
- HistoryEntry, SetCurrentCmd, ClearCurrentCmd, SetupSignals
- getStatus, printStatus

## Verification
`go build ./... && go test -v ./...` — all 26 existing tests pass with updated signatures.
