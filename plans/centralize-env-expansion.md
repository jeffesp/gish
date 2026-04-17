# Plan: Centralize Env Var Expansion for All Commands

## Context

Currently only `echo` supports env var expansion (`$VAR`), via inline logic in `builtinEcho`. Other builtins like `cd`, `set`, `unset`, and external commands all receive raw token values with no expansion. This means `cd $HOME` or `set FOO $BAR` don't work as expected.

The fix: expand env vars centrally in `execLine` after tokenization but before dispatch, so **every command** (builtins + external) gets expansion for free.

## Approach

### 1. Add `expandToken` helper to `token.go`

```go
func expandToken(t Token) Token {
    if t.Kind == TokenSingleQuoted {
        return t
    }
    return Token{Kind: t.Kind, Value: os.ExpandEnv(t.Value)}
}

func expandTokens(tokens []Token) []Token {
    out := make([]Token, len(tokens))
    for i, t := range tokens {
        out[i] = expandToken(t)
    }
    return out
}
```

- Uses `os.ExpandEnv` which handles `$VAR`, `${VAR}`, and `$$` (literal `$`) — strictly better than the current `echo` logic which only handles `$VAR` at the start of a token
- Respects `TokenSingleQuoted` (no expansion), matching shell semantics
- Expands `TokenWord` and `TokenDoubleQuoted` tokens

### 2. Call `expandTokens` in `execLine` (`repl.go:90-96`)

After `tokenize(line)` succeeds, expand all tokens before storing them in `ctx.Tokens`:

```go
tokens, err := tokenize(line)
if err != nil { ... }
tokens = expandTokens(tokens)   // <-- add this line
ctx.Line = line
ctx.Tokens = tokens
```

### 3. Simplify `builtinEcho` (`builtins.go:68-82`)

Remove the inline expansion logic since it's now handled centrally:

```go
func builtinEcho(ctx *ExecCtx) error {
    fmt.Fprintln(ctx.Out, strings.Join(tokenValues(ctx.Args()), " "))
    return nil
}
```

### 4. Update tests

- **Echo tests** (`builtins_test.go`): The `testCtx` helper feeds tokens directly to builtins, bypassing `execLine`. Existing echo tests that test `$VAR` expansion with `TokenWord`/`TokenDoubleQuoted` kinds need to be pre-expanded (since echo no longer does its own expansion). Alternatively, add integration-level tests that go through `execLine`.
- **Add expansion-specific tests** for `expandToken`/`expandTokens` in a new test or in `builtins_test.go` covering: bare `$VAR`, `${VAR}`, single-quoted no-expand, double-quoted expand, embedded `$VAR` in text like `hello$USER`.
- **Add cd expansion test**: `cd $HOME` should work.

## Files to Modify

- `token.go` — add `expandToken`, `expandTokens`
- `repl.go:90-96` — call `expandTokens` after tokenize
- `builtins.go:68-82` — simplify `builtinEcho`
- `builtins_test.go` — update echo tests, add expansion tests

## Verification

1. `go build ./...` — compiles
2. `go test ./...` — all tests pass
3. Manual smoke test in gish:
   - `echo $HOME` → prints home dir
   - `echo '$HOME'` → prints literal `$HOME`
   - `cd $HOME` → changes to home dir
   - `set FOO bar` then `echo $FOO` → prints `bar`
   - `set DEST /tmp` then `cd $DEST` → changes to /tmp
   - External: `ls $HOME` → lists home dir
