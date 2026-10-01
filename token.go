package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type TokenKind int

const (
	TokenWord         TokenKind = iota // unquoted bare word
	TokenSingleQuoted                  // 'text' — no expansion
	TokenDoubleQuoted                  // "text" — expansion later
	TokenPipe                          // literal pipe char: |
	TokenMergePipe                     // literal pipe + amp: |&
	TokenBackground                    // literal amp - &, only valid at EOL
)

// Token is a single lexical unit of a command line.
//
// Escapes holds the byte offsets within Value that were
// backslash-escaped in the input. The tokenizer strips the backslashes but
// records the positions, so later expansion passes leave those bytes
// literal (a \$HOME is not a variable reference).
type Token struct {
	Kind    TokenKind
	Value   string
	Escapes []int
}

// homeDir returns the current user's home directory: $HOME, falling back
// to the OS's idea of it (Windows and some minimal environments don't set
// HOME). Returns "" if neither is available.
func homeDir() string {
	if dir := os.Getenv("HOME"); dir != "" {
		return dir
	}
	dir, _ := os.UserHomeDir()
	return dir
}

// expandTilde expands a leading ~ or ~user in path: "~" and "~/rest" use
// the current user's home directory, "~name" and "~name/rest" use name's.
// A path that doesn't start with ~, or names a user that can't be found,
// is returned unchanged.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	name, rest := path[1:], ""
	if i := strings.IndexByte(name, '/'); i >= 0 {
		name, rest = name[:i], name[i:]
	}

	var home string
	if name == "" {
		home = homeDir()
	} else if u, err := user.Lookup(name); err == nil {
		home = u.HomeDir
	}
	if home == "" {
		return path
	}
	return home + rest
}

// expandTildes applies expandTilde to each unquoted word whose leading ~
// wasn't backslash-escaped. It must run before expandVars and expandGlobs,
// as in bash, so a $VAR whose value starts with ~ stays literal. Escape
// offsets are shifted to follow the text the replacement moved.
func expandTildes(tokens []Token) []Token {
	out := make([]Token, len(tokens))
	for i, t := range tokens {
		out[i] = t
		if t.Kind != TokenWord || !strings.HasPrefix(t.Value, "~") || slices.Contains(t.Escapes, 0) {
			continue
		}
		// Only the text before the first slash names the home directory,
		// so only escapes after it matter for the shift.
		expanded := expandTilde(t.Value)
		if expanded == t.Value {
			continue
		}
		prefixLen := len(t.Value)
		if j := strings.IndexByte(t.Value, '/'); j >= 0 {
			prefixLen = j
		}
		delta := len(expanded) - len(t.Value)
		var escapes []int
		for _, e := range t.Escapes {
			if e >= prefixLen {
				e += delta
			}
			escapes = append(escapes, e)
		}
		out[i] = Token{Kind: TokenWord, Value: expanded, Escapes: escapes}
	}
	return out
}

func expandToken(t Token) Token {
	if t.Kind == TokenWord || t.Kind == TokenDoubleQuoted {
		return Token{Kind: t.Kind, Value: expandValue(t.Value, t.Escapes)}
	}
	return t
}

// expandVar resolves $VAR and ${VAR} during token expansion, matching the
// behavior of os.ExpandEnv. $?, like in classic shells, resolves to the
// exit code of the most recently executed command.
func expandVar(key string) string {
	if key == "?" {
		return strconv.Itoa(lastExitCode)
	}
	return os.Getenv(key)
}

// expandValue expands $var and ${var} references in value, mirroring
// os.Expand, except that bytes at positions in escapes are left literal:
// a backslash-escaped $ does not begin a variable reference.
//
// Tokens without escapes go through os.Expand directly, so behavior is
// byte-for-byte the standard library's for all unescaped input.
func expandValue(value string, escapes []int) string {
	if len(escapes) == 0 {
		return os.Expand(value, expandVar)
	}
	mask := make([]bool, len(value))
	for _, e := range escapes {
		if e >= 0 && e < len(value) {
			mask[e] = true
		}
	}

	var buf []byte
	i := 0
	for j := 0; j < len(value); j++ {
		if value[j] == '$' && !mask[j] && j+1 < len(value) {
			if buf == nil {
				buf = make([]byte, 0, 2*len(value))
			}
			buf = append(buf, value[i:j]...)
			name, w := shellName(value[j+1:])
			if name == "" && w > 0 {
				// Invalid syntax (e.g. a dangling ${): eat the
				// characters, as os.Expand does.
			} else if name == "" {
				// $ not followed by a name; leave the dollar sign untouched.
				buf = append(buf, value[j])
			} else {
				buf = append(buf, expandVar(name)...)
			}
			j += w
			i = j + 1
		}
	}
	if buf == nil {
		return value
	}
	return string(buf) + value[i:]
}

// shellName returns the variable name beginning at s and the number of
// bytes consumed to extract it. Copied from the os package's unexported
// getShellName so expandValue matches os.Expand exactly.
func shellName(s string) (string, int) {
	switch {
	case s[0] == '{':
		if len(s) > 2 && shellSpecialVar(s[1]) && s[2] == '}' {
			return s[1:2], 3
		}
		// Scan to closing brace
		for i := 1; i < len(s); i++ {
			if s[i] == '}' {
				if i == 1 {
					return "", 2 // Bad syntax; eat "${}"
				}
				return s[1:i], i + 1
			}
		}
		return "", 1 // Bad syntax; eat "${"
	case shellSpecialVar(s[0]):
		return s[0:1], 1
	}
	// Scan alphanumerics.
	var i int
	for i = 0; i < len(s) && isAlphaNum(s[i]); i++ {
	}
	return s[:i], i
}

// shellSpecialVar and isAlphaNum are copies of the os package predicates
// used by getShellName.
func shellSpecialVar(c uint8) bool {
	switch c {
	case '*', '#', '$', '@', '!', '?', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func isAlphaNum(c uint8) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func expandVars(tokens []Token) []Token {
	out := make([]Token, len(tokens))
	for i, t := range tokens {
		out[i] = expandToken(t)
	}
	return out
}

func expandGlobs(tokens []Token) []Token {
	var out []Token
	for _, t := range tokens {
		// A word containing backslash-escaped characters is a literal
		// filename, never a pattern, matching bash (f\*.txt does not
		// glob).
		if t.Kind == TokenWord && len(t.Escapes) == 0 {
			res, err := filepath.Glob(t.Value)
			if err != nil || len(res) == 0 {
				out = append(out, t)
				continue
			}

			// Like standard shells, exclude dotfiles unless the
			// pattern itself starts with a dot (e.g. ".*").
			patBase := filepath.Base(t.Value)
			includeDot := len(patBase) > 0 && patBase[0] == '.'

			for _, val := range res {
				base := filepath.Base(val)
				if !includeDot && len(base) > 0 && base[0] == '.' {
					continue
				}
				out = append(out, Token{Kind: TokenWord, Value: val})
			}
		} else {
			out = append(out, t)
		}
	}
	return out
}

func tokenize(line string) ([]Token, error) {
	var tokens []Token
	var cur strings.Builder
	var escapes []int
	inQuote := false
	quoteChar := byte(0)
	curKind := TokenWord

	// emit appends the token being built and resets the builder.
	// escapes is reassigned rather than reset in place so the slice
	// stored in the emitted token isn't aliased by later appends.
	emit := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, Token{Kind: curKind, Value: cur.String(), Escapes: escapes})
		}
		cur.Reset()
		escapes = nil
		curKind = TokenWord
	}

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case !inQuote && ch == '\\':
			// A backslash escapes the next character, making it
			// literal: it can't open a quote, be an operator, or start
			// a $variable.
			if i+1 < len(line) {
				i++
				curKind = TokenWord
				cur.WriteByte(line[i])
				escapes = append(escapes, cur.Len()-1)
			} else {
				// Trailing backslash at end of line: kept as a literal
				// for now; this becomes line continuation once
				// multi-line input exists.
				curKind = TokenWord
				cur.WriteByte('\\')
			}
		case !inQuote && (ch == '\'' || ch == '"'):
			if cur.Len() == 0 {
				if ch == '\'' {
					curKind = TokenSingleQuoted
				} else {
					curKind = TokenDoubleQuoted
				}
			} else {
				curKind = TokenWord
			}
			inQuote = true
			quoteChar = ch
		case inQuote && quoteChar == '"' && ch == '\\' && i+1 < len(line) &&
			(line[i+1] == '"' || line[i+1] == '$' || line[i+1] == '\\'):
			// Inside double quotes, a backslash is special only before
			// ", $, and itself (bash parity); elsewhere it is a plain
			// character.
			i++
			cur.WriteByte(line[i])
			escapes = append(escapes, cur.Len()-1)
		case inQuote && ch == quoteChar:
			inQuote = false
		case !inQuote && (ch == ' ' || ch == '\t'):
			emit()
		case !inQuote && ch == '|':
			emit()
			if i < len(line)-1 && line[i+1] == '&' {
				i = i + 1
				tokens = append(tokens, Token{Kind: TokenMergePipe, Value: "|&"})
			} else {
				tokens = append(tokens, Token{Kind: TokenPipe, Value: string(ch)})
			}
		case !inQuote && ch == '&' && i == len(line)-1:
			tokens = append(tokens, Token{Kind: TokenBackground, Value: "&"})
		default:
			if !inQuote {
				curKind = TokenWord
			}
			cur.WriteByte(ch)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unclosed quote")
	}
	emit()
	return tokens, nil
}

func tokenValues(tokens []Token) []string {
	vals := make([]string, len(tokens))
	for i, t := range tokens {
		vals[i] = t.Value
	}
	return vals
}

func parseTokens(tokens []Token, line string) (Executable, bool, error) {
	var indices []int
	for i, t := range tokens {
		if t.Kind == TokenPipe || t.Kind == TokenMergePipe {
			indices = append(indices, i)
		}
	}
	background := false

	if tokens[len(tokens)-1].Kind == TokenBackground {
		background = true
		tokens = tokens[:len(tokens)-1]
	}

	if len(indices) == 0 {
		return &Command{Tokens: tokens, Line: line}, background, nil
	}

	// make sure pipes are in valid places
	if tokens[0].Kind == TokenPipe || tokens[0].Kind == TokenMergePipe {
		return nil, background, fmt.Errorf(": cannot start with a pipe")
	}
	if tokens[len(tokens)-1].Kind == TokenPipe || tokens[len(tokens)-1].Kind == TokenMergePipe {
		return nil, background, fmt.Errorf(": cannot end with a pipe")
	}
	for i := 1; i < len(indices); i++ {
		if indices[i] == indices[i-1]+1 {
			return nil, background, fmt.Errorf(": consecutive pipes")
		}
	}

	stages := make([]*Command, 0, len(indices)+1)
	mergeErr := make([]bool, 0, len(indices))
	prev := 0
	for _, idx := range indices {
		stages = append(stages, &Command{Tokens: tokens[prev:idx]})
		mergeErr = append(mergeErr, tokens[idx].Kind == TokenMergePipe)
		prev = idx + 1
	}
	stages = append(stages, &Command{Tokens: tokens[prev:]})
	return &Pipeline{Stages: stages, MergeErr: mergeErr}, background, nil
}
