# Plan: Typed tokens + double-quote support

## Context
The tokenizer currently returns `[]string`, losing all information about how each token was formed. Adding double-quote support is the immediate trigger, but the bigger goal is to attach kind metadata to tokens so the codebase can grow toward a real parser — eventually supporting things like env-var expansion, subcommand substitution, pipelines, and non-string data types. This is the right time to introduce a `Token` type before more builtins and execution logic accumulates.

## Package discussion

Two external packages were investigated:

- **`github.com/google/shlex`** — lightweight, but its token kinds are `WordToken / SpaceToken / CommentToken / UnknownToken`. It doesn't distinguish bare word from single-quoted from double-quoted, which is exactly the information we need. Not a fit.
- **`mvdan.cc/sh/v3/syntax`** — full POSIX shell AST parser. Rich token system, handles everything. But it's designed around POSIX shell semantics; if gish eventually wants non-shell data types (numbers, structured data, etc.) the POSIX model becomes a constraint rather than a help. Also significant API surface to adopt.

**Recommendation: custom `Token` type.** The tokenizer is small, the requirements are clear, and total control over the kind enum is exactly what's needed to extend toward non-shell data types later. No new dependency.

## Token type design

New file `token.go`:

```go
type TokenKind int

const (
    TokenWord         TokenKind = iota // unquoted bare word
    TokenSingleQuoted                  // 'text' — no expansion
    TokenDoubleQuoted                  // "text" — expansion later
)

type Token struct {
    Kind  TokenKind
    Value string
}
```

Keeping it as a flat struct (not an interface) for now — easy to switch later when "other data types" become concrete.

## Files to modify

- `token.go` (new) — `Token`, `TokenKind`, constants
- `repl.go` — update `tokenize` to return `[]Token`, add double-quote branch
- `builtins.go` — update `BuiltinFunc` signature to `func([]Token, io.Writer) error`, update all builtins to use `.Value`

## Implementation

### token.go
Define `TokenKind` and `Token` as above. No other logic yet — this file grows as the parser does.

### repl.go — tokenize

Track kind per-token, defaulting to `TokenWord`. A token gets upgraded to `TokenSingleQuoted` or `TokenDoubleQuoted` only if it was formed entirely by a single quote pair — no mixed unquoted characters. A word like `foo'bar'` stays `TokenWord`. Only a standalone `'foo bar'` or `"foo bar"` gets the quoted kind. This matches how most shells represent mixed tokens at the AST level.

```go
func tokenize(line string) ([]Token, error) {
    var tokens []Token
    var cur strings.Builder
    inQuote := false
    quoteChar := byte(0)
    pureQuote := false      // true if token started with a quote and has no bare chars yet
    curKind := TokenWord

    for i := 0; i < len(line); i++ {
        ch := line[i]
        switch {
        case !inQuote && (ch == '\'' || ch == '"'):
            if cur.Len() == 0 {
                pureQuote = true
                if ch == '\'' {
                    curKind = TokenSingleQuoted
                } else {
                    curKind = TokenDoubleQuoted
                }
            } else {
                pureQuote = false
                curKind = TokenWord
            }
            inQuote = true
            quoteChar = ch
        case inQuote && ch == quoteChar:
            inQuote = false
        case !inQuote && (ch == ' ' || ch == '\t'):
            if cur.Len() > 0 {
                tokens = append(tokens, Token{curKind, cur.String()})
                cur.Reset()
                curKind = TokenWord
                pureQuote = false
            }
        default:
            if !inQuote {
                pureQuote = false
                curKind = TokenWord
            }
            cur.WriteByte(ch)
        }
    }
    if inQuote {
        return nil, fmt.Errorf("unclosed quote")
    }
    if cur.Len() > 0 {
        tokens = append(tokens, Token{curKind, cur.String()})
    }
    return tokens, nil
}
```

### repl.go — execLine
```go
func tokenValues(tokens []Token) []string {
    vals := make([]string, len(tokens))
    for i, t := range tokens {
        vals[i] = t.Value
    }
    return vals
}

func execLine(...) {
    tokens, err := tokenize(line)
    ...
    name := tokens[0].Value
    args := tokens[1:]
    if fn, ok := Builtins[name]; ok {
        fn(args, out)
        ...
    }
    cmd := exec.Command(name, tokenValues(args)...)
    ...
}
```

### builtins.go
- Change `BuiltinFunc` to `func(args []Token, w io.Writer) error`
- All builtins: replace `args[i]` with `args[i].Value`, `arg` with `arg.Value`
- `builtinEcho`: currently expands `$VAR` on all args. With typed tokens, skip expansion for `TokenSingleQuoted` args (standard shell behavior). Double-quoted expansion is future work — treat same as bare word for now.

## Verification
- `echo 'hello world'` → single argument `hello world` printed
- `echo "hello world"` → single argument `hello world` printed
- `cd 'my dir'` → changes to directory named `my dir`
- `echo '$HOME'` → prints literal `$HOME` (single-quoted, no expansion)
- `echo "$HOME"` → expands (double-quoted expansion future work, should not regress)
- `echo unclosed'quote` → error: unclosed quote
