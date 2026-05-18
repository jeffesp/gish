# Alias Support for gish

## Context

gish has no aliases today. The user wants standard shell-style aliases (`ll` → `ls -la`) registered from `init.js`. Two decisions framed the design:

- **No `alias` builtin.** Aliases are configuration; `init.js` is the natural place. Interactive use is satisfied by `js gish.alias("ll", "ls -la")` via the existing [scripting.go](../scripting.go) `js` builtin. A dedicated builtin would duplicate `name=value` parsing that JS string literals already give us.
- **Expand pre-parse, not pre-exec.** Expanding inside [command.go](../command.go) `Command.Exec`/`Start` only swaps a single token, breaking multi-token aliases (`'ls -la'`) and aliases that introduce pipes. Expanding in [repl.go](../repl.go) `execLine` between `expandGlobs` and `parseTokens` lets the alias body re-tokenize and contribute multiple tokens, including `TokenPipe`, before the parser splits stages.

## Design

### New file: `aliases.go`

Holds the registry and the expansion pass.

```go
var aliases = struct {
    sync.RWMutex
    m map[string]string
}{m: map[string]string{}}

func setAlias(name, body string) error  // validates name (no spaces/pipes/empty), stores body
func unsetAlias(name string)
func getAlias(name string) (string, bool)

// expandAliases walks tokens, replacing the first TokenWord at each command
// position (start of stream and after every TokenPipe) with the re-tokenized
// alias body. Tracks visited names per pass to prevent infinite recursion
// (matches bash: `alias ls='ls --color'` works).
func expandAliases(tokens []Token) ([]Token, error)
```

Expansion rules:
- Only `TokenWord` at command position is candidate (single/double quoted tokens are literal, matching bash).
- If alias body fails `tokenize()`, return the error from `execLine` like other expansion errors.
- A `visited map[string]bool` per call prevents cycles; once `ls` has been expanded, the resulting `ls` token is not re-expanded.

### `scripting.go` — register JS API

In `InitScripting`, after the existing `gish.register` / `gish.setPrompt` setup, add two methods on the gish object using the same `jsmu`-protected pattern:

- `gish.alias(name, body)` → calls `setAlias`. Returns the previous body (or `undefined`) for symmetry with `setPrompt`.
- `gish.unalias(name)` → calls `unsetAlias`.

These do **not** go through `RegisterBuiltin`; they're plain Go-backed methods on the gish JS object. The `js` builtin already lets users invoke them interactively.

### `repl.go` — wire the expansion pass

In `execLine`, after `expandGlobs` and before `parseTokens`:

```go
tokens, err = expandAliases(tokens)
if err != nil {
    fmt.Fprintf(ctx.ErrOut, "%v\n", err)
    return
}
```

That's the only call site. `parseTokens` and downstream code are unchanged.

## Files to modify

- `aliases.go` — new file.
- [scripting.go](../scripting.go) — register `gish.alias` and `gish.unalias` inside `InitScripting`.
- [repl.go:42-44](../repl.go#L42-L44) — insert `expandAliases` call between `expandGlobs` and `parseTokens`.

## What's intentionally out of scope

- No `alias` / `unalias` builtin (rationale above).
- No listing API (`gish.aliases` getter) — easy to add later if wanted; left out to keep surface small.
- No precedence override of builtins; aliases run before builtin lookup, so `alias cd='echo blocked'` will shadow the `cd` builtin. This matches bash and is the natural consequence of expanding pre-parse — flagging it so it's not a surprise.

## Verification

1. Build: `go build ./...`
2. Start `gish`, then in the REPL:
   - `js gish.alias("ll", "ls -la")` then `ll` → should run `ls -la`.
   - `js gish.alias("ngrep", "grep -v '^#'")` then `cat somefile | ngrep` → pipeline still parses; alias contributes a stage.
   - `js gish.alias("ls", "ls --color=auto")` then `ls` → expands once, doesn't infinite-loop.
   - `js gish.unalias("ll")` then `ll` → "command not found".
   - Single-quoted: `'ll'` should NOT expand (tokenizer gives `TokenSingleQuoted`).
3. Move the registration into `~/.config/gish/init.js` and confirm aliases survive a fresh shell.
