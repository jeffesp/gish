# Plan: Output Redirection

## Context

Gish has pipelines (`|`) but no file redirection. Adding `>`, `>>`, `2>`, `<` etc. is a
fundamental shell capability. Since gish is a custom shell, we also support readable named
operators (inspired by nushell) and JS API equivalents.

### Mid-pipeline redirection: what other shells do

Per-stage redirection is supported (not just end-of-pipeline):

- `cmd1 2> /dev/null | cmd2` — redirect stderr of one stage, pipe stdout. Common, useful pattern.
- `cmd1 > file | cmd2` — in bash/fish, redirect wins, cmd2 gets EOF. In zsh with MULTIOS,
  output tees to both. **We follow bash semantics** — redirect wins, pipe gets nothing.
- Bash `|&` pipes both stdout+stderr. Fish has `2|&` for piping just stderr.

**Decision**: Redirections are per-command. Each command in a pipeline can have its own
redirections. The redirect replaces the stream — no implicit tee.

---

## Supported Operators

### File redirection

| Operation                  | POSIX syntax | Gish named syntax | JS API field           |
| -------------------------- | ------------ | ----------------- | ---------------------- |
| stdout to file (overwrite) | `>`          | `out>`            | `stdout: "path"`       |
| stdout to file (append)    | `>>`         | `out>>`           | `stdoutAppend: "path"` |
| stderr to file (overwrite) | `2>`         | `err>`            | `stderr: "path"`       |
| stderr to file (append)    | `2>>`        | `err>>`           | `stderrAppend: "path"` |
| both to file (overwrite)   | `&>`         | `out+err>`        | `output: "path"`       |
| both to file (append)      | `&>>`        | `out+err>>`       | `outputAppend: "path"` |
| stdin from file            | `<`          | `in<`             | `stdin: "path"`        |

POSIX and named forms produce the **same token kind** — interchangeable syntax. Long names only
(no short forms like `o>` / `e>`).

### Pipe operators

| Operation          | Syntax | Equivalent in bash |
| ------------------ | ------ | ------------------ |
| pipe stdout        | `\|`   | `\|` (existing)    |
| pipe stdout+stderr | `\|&`  | `\|&` or `2>&1 \|` |

`|&` is a new pipe operator that merges stderr into stdout before piping to the next command.
It's a pipeline separator like `|`, not a per-command redirect. In the token stream it becomes
`TokenMergePipe`.

---

## Implementation

### 1. New token kinds (`token.go`)

Add 8 constants after `TokenPipe`:

```go
TokenMergePipe         // |&  (pipe both stdout+stderr)
TokenRedirectOut       // >   or  out>
TokenRedirectAppend    // >>  or  out>>
TokenRedirectErr       // 2>  or  err>
TokenRedirectErrAppend // 2>> or  err>>
TokenRedirectAll       // &>  or  out+err>
TokenRedirectAllAppend // &>> or  out+err>>
TokenRedirectIn        // <   or  in<
```

### 2. Tokenizer changes (`token.go`)

**All redirect and merge-pipe operators require whitespace separation** — they are only
recognized at token boundaries (when `cur.Len() == 0`, i.e. after whitespace or at start of
line). `echo>file` is a single word token, not a redirect. You must write `echo > file`.

This keeps the tokenizer simple: a single check at the top of the loop, before word accumulation,
tries to match the longest operator prefix.

Add `tryRedirectOp(line string, i int) (TokenKind, int)` — match longest operator at position
`i`. Returns token kind and characters consumed, or `(0, 0)` if no match.

**Match table** (checked longest-first within each group):

All operators (both POSIX and named) use the same path since they all require token boundaries:

- `out+err>>`, `out+err>`, `out>>`, `out>`, `err>>`, `err>`, `in<`
- `|&`, `&>>`, `&>`, `2>>`, `2>`, `>>`, `>`, `<`

In `tokenize()`, when `cur.Len() == 0` and not in a quote, call `tryRedirectOp`. If it matches,
emit the token and advance `i`. Otherwise fall through to normal word accumulation.

Skip redirect/merge tokens in `expandToken()` (no variable expansion on operators).

### 3. Redirect data structures (new file: `redirect.go`)

```go
type RedirectKind int

const (
    RedirectOut       RedirectKind = iota // >  / out>
    RedirectAppend                        // >> / out>>
    RedirectErr                           // 2> / err>
    RedirectErrAppend                     // 2>> / err>>
    RedirectAll                           // &> / out+err>
    RedirectAllAppend                     // &>> / out+err>>
    RedirectIn                            // < / in<
)

type Redirect struct {
    Kind RedirectKind
    File string
}
```

### 4. Redirect extraction (`redirect.go`)

`extractRedirects(tokens []Token) ([]Token, map[int][]Redirect, error)`

Walks the token stream. Tracks pipe stage (incremented on `TokenPipe` or `TokenMergePipe`).
When a redirect token is found, consumes the next token as the filename. Returns cleaned tokens
(redirect operators and filenames removed) and a map of stage-index to redirects.

Errors: missing filename (`echo >`), redirect token as filename (`echo > >`).

### 5. Apply redirects during execution (`redirect.go`)

`applyRedirects(ctx *ExecCtx, redirects []Redirect) (*ExecCtx, func(), error)`

Opens files and returns a new `ExecCtx` with replaced In/Out/ErrOut. Returns a cleanup function
that closes all opened files. Sets `RunCmd = nil` on the new context when any redirect is present.

File open modes:

- Overwrite: `os.Create` (0644)
- Append: `os.OpenFile` with `O_APPEND|O_CREATE|O_WRONLY` (0644)
- Input: `os.Open`
- `RedirectAll`/`RedirectAllAppend`: same file handle for both `Out` and `ErrOut`

### 6. Merge pipe (`|&`) in pipeline execution (`command.go`)

**`parseTokens` changes**: Split on both `TokenPipe` and `TokenMergePipe`. The `Pipeline` struct
gets a per-stage flag indicating whether that stage uses a merge pipe:

```go
type Pipeline struct {
    Stages    []*Command
    MergeErr  []bool  // MergeErr[i] == true means stage i's stderr merges into the pipe to stage i+1
}
```

`MergeErr` has length `len(Stages)-1` (one flag per pipe operator between stages).

**`Pipeline.Exec` changes**: When `MergeErr[i]` is true, set `localCtx.ErrOut = pw` (the pipe
write end) instead of `ctx.ErrOut`. This merges stderr into the pipe alongside stdout.

### 7. Wire into execution flow

**`repl.go` — `execLine` changes:**

Insert `extractRedirects` after `expandAliases`, before `parseTokens`:

```
tokenize → expandVars → expandGlobs → expandAliases → extractRedirects → parseTokens → exec
```

`attachRedirects(exe, redirectMap)` sets `cmd.Redirects` on each `Command` after parsing.

**`command.go` — `Command` changes:**

Add `Redirects []Redirect` field to `Command`.

`Command.Exec`: call `applyRedirects` at top, `defer cleanup()`, use `rCtx`.

`Command.Start`: call `applyRedirects` at top, cleanup in wait function closure.

**Pipeline.Exec**: no redirect changes needed (handled in `Start`). Only change is `|&` support
via `MergeErr` flag as described above.

### 8. JavaScript API (`scripting.go`)

Optional redirect options object as third argument to `gish.exec` and fourth to `gish.spawn`:

```javascript
var r = gish.exec("cmd", ["arg1"], {
  stdout: "/path/to/file", // > file
  stdoutAppend: "/path/to/file", // >> file
  stderr: "/path/to/file", // 2> file
  stderrAppend: "/path/to/file", // 2>> file
  stdin: "/path/to/file", // < file
  output: "/path/to/file", // &> file (both streams)
  outputAppend: "/path/to/file", // &>> file
});

gish.spawn("cmd", ["arg1"], onLine, {
  stderr: "/dev/null",
});
```

Check for the options arg, extract fields, open files, wire to `exec.Cmd`, close after.

---

## Files to modify

| File               | Changes                                                                                                                                                                |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `token.go`         | 8 new `TokenKind` constants, `tryRedirectOp()`, token-boundary check in loop, `expandToken` skip                                                                       |
| `redirect.go`      | **New.** `RedirectKind`, `Redirect`, `extractRedirects`, `applyRedirects`, `attachRedirects`                                                                           |
| `command.go`       | `Redirects` on `Command`, `MergeErr` on `Pipeline`, `applyRedirects` in `Exec`/`Start`, merge-pipe wiring in `Pipeline.Exec`, `parseTokens` splits on `TokenMergePipe` |
| `repl.go`          | Insert `extractRedirects` + `attachRedirects` in `execLine`                                                                                                            |
| `scripting.go`     | Redirect options on `gish.exec` and `gish.spawn`                                                                                                                       |
| `tok_test.go`      | Tokenizer tests for all operators                                                                                                                                      |
| `redirect_test.go` | **New.** Tests for extraction, application, pipeline+redirect combos                                                                                                   |

## Implementation order

1. **Tokenizer** — 8 new token kinds, `tryRedirectOp`, tokenizer loop, tests
2. **Data structures + extraction** — `redirect.go` with types and `extractRedirects`
3. **Apply redirects** — `applyRedirects` function, unit tests
4. **Wire into execution** — `Command.Redirects`, `execLine` flow, `Exec`/`Start` changes
5. **Merge pipe** — `MergeErr` on `Pipeline`, `parseTokens` + `Pipeline.Exec` changes
6. **Integration tests** — file redirects, pipeline combos, merge pipe
7. **JS API** — redirect options on `gish.exec`/`gish.spawn`

## Edge cases and decisions

- **Whitespace required**: all operators must be whitespace-separated. `echo>file` is a single
  word, not a redirect. Write `echo > file`. This keeps the tokenizer simple — operators are
  only matched at token boundaries.
- **Multiple redirects to same stream**: last one wins (left-to-right, bash behavior)
- **Glob expansion on redirect filenames**: happens (extractRedirects runs after expandGlobs).
  Matches bash. User error if glob expands to multiple files — not special-cased for v1.
- **Aliases introducing redirects**: works (extractRedirects runs after expandAliases)
- **`RunCmd` callback**: nil when redirects present — direct I/O, no terminal-restore wrapper
- **`2` as a regular arg**: `2>` only matches at token boundaries, so `echo 2 > file` is
  word `echo`, word `2`, redirect `>`, word `file` — the `2` is an arg, not part of the redirect.
  `echo 2> file` is word `echo`, redirect `2>`, word `file` — correct.
- **`|&` vs `> |`**: `|&` is a single merge-pipe token. `> |` with space is redirect `>` then
  pipe `|` — different meaning, which is a parse error (redirect missing filename).

## Verification

1. `go test ./...` — all existing + new tests pass
2. Manual tests in gish:
   - `echo hello > /tmp/gish-test.txt` then `cat /tmp/gish-test.txt`
   - `echo world >> /tmp/gish-test.txt` then `cat /tmp/gish-test.txt` (two lines)
   - `ls nonexistent 2> /tmp/gish-err.txt` then `cat /tmp/gish-err.txt`
   - `ls nonexistent &> /tmp/gish-both.txt`
   - `cat < /tmp/gish-test.txt`
   - Named: `echo test out> /tmp/gish-named.txt`
   - Named: `ls nonexistent err> /tmp/gish-named-err.txt`
   - Pipeline + redirect: `ls nonexistent 2> /dev/null | cat` (no error output)
   - Pipeline + redirect: `echo hello > /tmp/gish-pipe.txt | cat` (cat gets nothing)
   - Merge pipe: `ls nonexistent |& cat` (cat sees the error message on stdin)
   - Merge pipe: `ls nonexistent |& grep "No such"` (grep matches stderr output)
   - JS: `js gish.exec("echo", ["hello"], {stdout: "/tmp/gish-js.txt"})`
