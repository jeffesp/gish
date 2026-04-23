package main

import (
	"fmt"
	"os"
	"strings"
)

type TokenKind int

const (
	TokenWord         TokenKind = iota // unquoted bare word
	TokenSingleQuoted                  // 'text' — no expansion
	TokenDoubleQuoted                  // "text" — expansion later
	TokenPipe                          // literal pipe char: |
)

type Token struct {
	Kind  TokenKind
	Value string
}

func expandToken(t Token) Token {
	if t.Kind == TokenSingleQuoted || t.Kind == TokenPipe {
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

func tokenize(line string) ([]Token, error) {
	var tokens []Token
	var cur strings.Builder
	inQuote := false
	quoteChar := byte(0)
	curKind := TokenWord

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
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
		case inQuote && ch == quoteChar:
			inQuote = false
		case !inQuote && (ch == ' ' || ch == '\t'):
			if cur.Len() > 0 {
				tokens = append(tokens, Token{curKind, cur.String()})
				cur.Reset()
				curKind = TokenWord
			}
		case !inQuote && ch == '|':
			if cur.Len() > 0 {
				tokens = append(tokens, Token{curKind, cur.String()})
				cur.Reset()
				curKind = TokenWord
			}
			tokens = append(tokens, Token{TokenPipe, string(ch)})
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
	if cur.Len() > 0 {
		tokens = append(tokens, Token{curKind, cur.String()})
	}
	return tokens, nil
}

func tokenValues(tokens []Token) []string {
	vals := make([]string, len(tokens))
	for i, t := range tokens {
		vals[i] = t.Value
	}
	return vals
}

func parseTokens(tokens []Token, line string) (Executable, error) {
	var indices []int
	for i, t := range tokens {
		if t.Kind == TokenPipe {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return &Command{Tokens: tokens, Line: line}, nil
	}

	// make sure pipe are in valid places
	if tokens[0].Kind == TokenPipe {
		return nil, fmt.Errorf("|: cannot start with a pipe")
	}
	if tokens[len(tokens)-1].Kind == TokenPipe {
		return nil, fmt.Errorf("|: cannot end with a pipe")
	}
	for i := 1; i < len(indices); i++ {
		if indices[i] == indices[i-1]+1 {
			return nil, fmt.Errorf("|: consecutive pipes")
		}
	}

	stages := make([]*Command, 0, len(indices)+1)
	prev := 0
	for _, idx := range indices {
		stages = append(stages, &Command{Tokens: tokens[prev:idx]})
		prev = idx + 1
	}
	stages = append(stages, &Command{Tokens: tokens[prev:]})
	return &Pipeline{Stages: stages}, nil
}
