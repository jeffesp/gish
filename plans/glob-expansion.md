# Plan: Glob Expansion in Command Arguments

## Context
Gish currently expands environment variables in tokens but does not expand file path globs. When a user types `ls *.go`, the literal `*.go` is passed to the external command. While many external commands handle globs themselves, the shell should expand them — this is standard shell behavior and is needed for builtins and consistent behavior.

## Approach

Add a `expandGlobs` step in the token processing pipeline in `token.go`, called from `execLine` in `repl.go` after `expandTokens` and before `parseTokens`.

### Files to modify
- `token.go` — add `expandGlobs` function
- `repl.go` — wire it into `execLine`
- `tok_test.go` — add tests

### Implementation

**1. `expandGlobs(tokens []Token) []Token` in `token.go`**

- Iterate over tokens. For each token:
  - Skip `TokenSingleQuoted`, `TokenDoubleQuoted`, and `TokenPipe` tokens (quoting suppresses glob expansion, matching bash behavior)
  - For `TokenWord` tokens, call `filepath.Glob(token.Value)`
  - If matches are found, replace the single token with one `TokenWord` per match
  - If no matches (or error), keep the original token as-is (bash default behavior with `nullglob` off)
- Return the new token slice

**2. Wire into `execLine` in `repl.go` (line 41)**

Add after `expandTokens`:
```go
tokens = expandTokens(tokens)
tokens = expandGlobs(tokens)    // new line
```

**3. Tests in `tok_test.go`**

- Test that a glob pattern expands to matching files
- Test that single-quoted globs are not expanded
- Test that double-quoted globs are not expanded
- Test that a non-matching glob passes through unchanged
- Use a temp directory with known files for deterministic tests

## Verification
- Run `go test ./...`
- Build and manually test: `echo *.go` should list Go files, `echo '*.go'` should print literal `*.go`
