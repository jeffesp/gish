# Plan: Pipelines

## Overview

Add two pipeline operators to gish, delivered in two phases:

- **Phase 1**: `|` — standard Unix pipe (text streaming between processes)
- **Phase 2**: `|>` — JSON pipe (JSONL streaming between stages, structured data flow)

Phase 1 includes a refactor of the execution model to introduce an `Executable` interface and separate per-command data from the execution environment. This lays the groundwork for Phase 2 and future features.

## Background

### How Unix pipes work

Shells use the `pipe(2)` syscall to create a kernel-managed buffer. All processes in a pipeline start concurrently and the kernel handles flow control — a writer blocks when the buffer is full, a reader blocks when it's empty. Data streams as it's produced; the downstream process does not wait for upstream to finish.

### Current state

- Tokenizer produces `[]Token` with three kinds: `TokenWord`, `TokenSingleQuoted`, `TokenDoubleQuoted`
- No operator tokens exist — `|` is currently treated as a word character
- `execLine()` handles a single command (builtin or external)
- `ExecCtx` has `In`, `Out`, `ErrOut`, `Line`, `Tokens`
- `BuiltinFunc` is `func(ctx *ExecCtx) error` — builtins read args from `ctx.Tokens`
- JS runtime has `gish.exec()` (capture output) and `gish.spawn()` (stream lines)

---

# Phase 1: Standard Pipes

## 1. Execution model refactor

### Separate Command from ExecCtx

Currently `ExecCtx` holds both the execution environment (I/O handles) and per-command data (`Tokens`, `Line`). Split these apart:

```go
// ExecCtx is the execution environment — I/O handles and shell-level callbacks.
// It does not hold per-command data.
type ExecCtx struct {
    In     io.Reader
    Out    io.Writer
    ErrOut io.Writer
    RunCmd func(*exec.Cmd) error // terminal restore/raw mode callback
}

// Command is a parsed command — the name, args, and raw line.
type Command struct {
    Tokens []Token
    Line   string
}

func (c *Command) Name() string    { return c.Tokens[0].Value }
func (c *Command) Args() []Token   { return c.Tokens[1:] }
```

`Name()` and `Args()` move from `ExecCtx` to `Command`.

### Executable interface

```go
type Executable interface {
    Exec(ctx *ExecCtx) error
}
```

`Command` and `Pipeline` both implement `Executable`.

**`Command.Start`**: launches the command (builtin or external) and returns a wait function. Builtin dispatch happens here — callers don't need to know what kind of command it is.

```go
func (c *Command) Start(ctx *ExecCtx) (wait func() error) {
    if fn, ok := Builtins[c.Name()]; ok {
        done := make(chan error, 1)
        go func() { done <- fn(c, ctx) }()
        return func() error { return <-done }
    }
    cmd := exec.Command(c.Name(), tokenValues(c.Args())...)
    cmd.Stdin = ctx.In
    cmd.Stdout = ctx.Out
    cmd.Stderr = ctx.ErrOut
    cmd.Start()
    return cmd.Wait
}
```

**`Command.Exec`**: starts and immediately waits.

```go
func (c *Command) Exec(ctx *ExecCtx) error {
    return c.Start(ctx)()
}
```

**`Pipeline.Exec`**: wires pipes, calls `Start` on each stage, and coordinates the wait functions (see section 4). It doesn't branch on builtin vs external — `Start` handles that.

### Builtin signature change

```go
// Before
type BuiltinFunc func(ctx *ExecCtx) error

// After
type BuiltinFunc func(cmd *Command, ctx *ExecCtx) error
```

Builtins read args from `cmd.Args()` / `cmd.Name()` and write to `ctx.Out` / `ctx.ErrOut`. Every existing builtin needs updating — it's mechanical: `ctx.Name()` → `cmd.Name()`, `ctx.Args()` → `cmd.Args()`, I/O stays on `ctx`.

### RunCmd on ExecCtx

The `runCmd` callback currently passed as a parameter to `execLine()` moves onto `ExecCtx`. The two REPL modes set it when constructing their context:

```go
// runRawREPL
termCtx := &ExecCtx{
    In: in, Out: t, ErrOut: t,
    RunCmd: func(cmd *exec.Cmd) error {
        cmd.Stdin = in
        cmd.Stdout = rawOut
        cmd.Stderr = rawErr
        term.Restore(fd, origState)
        err := cmd.Run()
        term.MakeRaw(fd)
        return err
    },
}

// runScannerREPL
ctx.RunCmd = func(cmd *exec.Cmd) error {
    cmd.Stdin = ctx.In
    cmd.Stdout = ctx.Out
    cmd.Stderr = ctx.ErrOut
    return cmd.Run()
}
```

### execLine simplification

```go
func execLine(line string, ctx *ExecCtx) {
    tokens := /* tokenize + expand */
    exe := parse(tokens, line)  // returns *Command or *Pipeline

    start := time.Now()
    err := exe.Exec(ctx)
    code := 0
    if err != nil {
        fmt.Fprintf(ctx.ErrOut, "error: %v\n", err)
        code = exitCode(err)
    }

    appendHistory(HistoryEntry{ ... })
}
```

Note: `execLine` no longer takes `runCmd` as a parameter — it's on `ctx`.

## 2. Tokenizer changes

Add one operator token kind (Phase 1 only needs `TokenPipe`; `TokenJSONPipe` is added in Phase 2):

```go
const (
    TokenWord         TokenKind = iota
    TokenSingleQuoted
    TokenDoubleQuoted
    TokenPipe                          // |
)
```

Tokenizer rules:

- `|` outside quotes flushes the current word (if any) and emits a `TokenPipe`
- Pipe tokens are never produced inside quotes
- Pipe tokens are not subject to variable expansion

## 3. Parser — pipeline splitting

```go
func parse(tokens []Token, line string) Executable
```

Walk the token list. If there are no `TokenPipe` tokens, return a `*Command`. Otherwise, split at pipe tokens and return a `*Pipeline`:

```go
type Pipeline struct {
    Stages []*Command
}
```

Validation:

- No empty stages (`| foo`, `foo |`, `foo | | bar` are errors)
- Each stage's first token must be a word (command name)

## 4. Standard pipe (`|`) execution

### Plumbing

Use `os.Pipe()` to connect stdout of stage N to stdin of stage N+1.

```
[stage 0] stdout → pipe → stdin [stage 1] stdout → pipe → stdin [stage 2]
```

- Stage 0 reads from `ctx.In` (terminal/script input)
- Last stage writes to `ctx.Out`
- All stages write stderr to `ctx.ErrOut`

All stages start concurrently — required because the pipe buffer is finite.

### Stage execution

`Pipeline.Exec` doesn't distinguish builtins from external commands — it calls `stage.Start(childCtx)` for each stage and collects the wait functions. `Start` handles the builtin-vs-external dispatch internally.

For each stage, `Pipeline.Exec` creates a child `ExecCtx` with `In`/`Out` wired to the appropriate pipe ends. After calling `Start`, the parent closes its copies of the pipe ends so EOF propagates correctly.

The terminal restore/raw mode callback (`ctx.RunCmd`) is not used for pipeline stages — `Start` bypasses it. Terminal state management is only relevant for the outermost I/O, which is handled by wiring stage 0's stdin and the last stage's stdout to the original handles.

### Error handling

When any stage exits non-zero or returns an error:

1. Record which stage failed and its error/exit code
2. Kill all still-running stages
3. Wait for all stages to finish
4. Return the error: `"pipeline stage N (command): exit code X"`

A goroutine per stage calls the wait function returned by `Start`. Results flow to a shared channel. A coordinator goroutine detects the first failure and initiates shutdown.

### Signal handling

The existing `SetCurrentCmd` / `ClearCurrentCmd` mechanism tracks one command. For pipelines, extend to track all commands so that SIGINT kills the whole pipeline. Options:

- Track a slice of `*exec.Cmd` instead of a single one
- Or track the pipeline's coordinator, which handles killing its children

## 5. JS runtime — `gish.pipe()`

Execute a standard text pipeline from JS:

```js
// Returns { stdout: string, stderr: string, exitCode: number }
let result = gish.pipe(
  ["grep", ["foo", "file.out"]],
  ["sort"],
  ["uniq"],
  ["wc", ["-l"]],
);
```

Captures the final stage's stdout as a string. Stderr from all stages is combined. Uses the same pipeline execution machinery as shell-level pipes.

## 6. History

A pipeline is one history entry. The `Command` field stores the full pipeline as typed. The `ExitCode` reflects the pipeline outcome (0 if all succeed, otherwise the exit code of the failed stage).

## 7. Phase 1 implementation order

1. **Execution model refactor**
   - a. `Command` struct with `Name()`, `Args()` methods — new file `command.go`
   - b. `Executable` interface
   - c. `Command.Exec()` — dispatch to builtin or external
   - d. Change `BuiltinFunc` signature to `func(cmd *Command, ctx *ExecCtx) error`
   - e. Update all builtins (`echo`, `cd`, `set`, `unset`, `env`, `clear`, `exec`, `js`, `history`)
   - f. Update JS `gish.register()` to construct `Command` for callbacks
   - g. Remove `Tokens`, `Line`, `Name()`, `Args()` from `ExecCtx`
   - h. Add `RunCmd` to `ExecCtx`, remove from `execLine` parameter
   - i. Update both REPL modes
   - j. Update `execLine` to use `parse()` → `Executable.Exec()`
2. **Tokenizer** — add `TokenPipe`, handle `|` in `tokenize()`
3. **Parser** — `parse()` function, pipeline splitting, validation
4. **Command.Start + Pipeline.Exec** — `Start` method (unified builtin/external dispatch), `os.Pipe()` wiring, concurrent execution, error coordination. Builtins in pipes fall out naturally from `Start`.
5. **Signal handling** — track all pipeline commands for SIGINT
6. **JS `gish.pipe()`**
7. **Tests** — tokenizer, parser, single-command exec, pipeline exec, error propagation, builtins in pipes, JS pipe

---

# Phase 2: JSON Pipes

_Depends on Phase 1 being complete._

## 1. Tokenizer addition

```go
TokenJSONPipe  // |>
```

Tokenizer change: when `|` is encountered, peek at the next character. If `>`, emit `TokenJSONPipe` and advance. Otherwise emit `TokenPipe`.

## 2. Parser update

`parse()` now also splits on `TokenJSONPipe`. A pipeline must use a single operator type — no mixing `|` and `|>` in one pipeline. Mixing is a parse error.

The `Pipeline` struct gains an operator field:

```go
type Pipeline struct {
    Stages   []*Command
    Operator TokenKind  // TokenPipe or TokenJSONPipe — uniform across pipeline
}
```

## 3. JSON pipe execution

The plumbing is identical to standard pipes — OS pipes, concurrent stages. The `Operator` field on `Pipeline` distinguishes behavior only where it matters (JS runtime integration, future validation).

At the shell level, `|>` and `|` use the same `os.Pipe()` machinery. The semantic distinction is for users and tooling: `|>` signals structured JSONL data flow.

## 4. Text-to-JSON bridge builtins

**`tojson`** — convert text lines to JSONL

```
grep foo file.out | tojson |> json-processing-tool
```

Default: each line becomes `{"line": "the text"}`.
Start simple, add flags later (`--split`, `--kv`, `--csv`).

**`fromjson`** — extract text from JSONL

```
json-tool |> fromjson .field | sort
```

Takes a field path, extracts that field from each JSON object, prints as text.
No argument: prints compact JSON (identity transform).

Both read from stdin line-by-line and write to stdout — they work as pipe stages naturally.

## 5. JS runtime — `gish.jpipe()`

JSON pipeline from JS. Stages can be commands OR JS functions:

```js
let results = gish.jpipe(
  ["grep", ["foo", "file.out"]],
  "tojson",
  (obj) => {
    if (obj.line.includes("important")) {
      return { ...obj, flagged: true };
    }
    return null; // filter out
  },
  ["external-json-tool", []],
);
```

Function stage semantics:

- Return an object → emit one JSONL line
- Return an array → emit one JSONL line per element (flatmap)
- Return `null` / `undefined` → emit nothing (filter)
- Throw → kill the pipeline, report error

JS function stages run in the single goja runtime. Since goja is single-threaded, only one JS function stage can be active at a time. This is acceptable for transforms; heavy processing should be an external tool.

## 6. Phase 2 implementation order

1. **`TokenJSONPipe`** — tokenizer update with `|>` lookahead
2. **Parser update** — split on `TokenJSONPipe`, uniform operator validation
3. **`Pipeline.Operator` field** — distinguish pipe type
4. **`tojson` builtin** — line wrapping, basic flags
5. **`fromjson` builtin** — field extraction
6. **JS `gish.jpipe()`** — command + function stages
7. **Tests**

---

## Open questions

1. **Mixed pipes**: Current proposal says no — a pipeline must be all `|` or all `|>`. The user explicitly bridges with `tojson`/`fromjson`. Revisit if this feels too restrictive in practice.

2. **Error semantics for malformed JSON in `|>`**: Current proposal: not the shell's problem. The downstream tool handles it. Alternative: shell validates and kills on invalid JSON. Tradeoff is strictness vs performance.

3. **`tojson` scope**: Start with default line wrapping only. Add `--split`, `--kv`, `--csv` as needed.

4. **JS function stages in `gish.jpipe`**: Single goja runtime means one JS function stage at a time. Acceptable for simple transforms. Could revisit with per-stage runtimes if this becomes a bottleneck.
