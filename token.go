package main

import "os"

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
