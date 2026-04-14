package main

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
