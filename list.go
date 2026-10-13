package main

import "fmt"

// isSegmentBoundary reports whether a token of kind k ends one command of a
// line and starts another: a pipe or a list operator.
func isSegmentBoundary(k TokenKind) bool {
	switch k {
	case TokenPipe, TokenMergePipe, TokenSemi, TokenAnd, TokenOr:
		return true
	}
	return false
}

// listItem is one pipeline of a command list, with the operator that joins
// it to the item before it (TokenSemi for the first item and after ';').
type listItem struct {
	Op     TokenKind
	Tokens []Token
}

// splitList splits a tokenized line on ';', '&&' and '||'. A trailing ';'
// is allowed; any other empty item ("; ls", "a && && b", "a &&") is a
// syntax error.
func splitList(tokens []Token) ([]listItem, error) {
	var items []listItem
	op := TokenSemi
	start := 0
	flush := func(end int, last bool) error {
		seg := tokens[start:end]
		if len(seg) == 0 {
			if last && op == TokenSemi && len(items) > 0 {
				return nil // trailing ';'
			}
			if last && len(items) == 0 {
				return nil // empty line
			}
			return fmt.Errorf("syntax error near %q", near(last, tokens, end))
		}
		items = append(items, listItem{Op: op, Tokens: seg})
		return nil
	}
	for i, t := range tokens {
		switch t.Kind {
		case TokenSemi, TokenAnd, TokenOr:
			if err := flush(i, false); err != nil {
				return nil, err
			}
			op = t.Kind
			start = i + 1
		}
	}
	if err := flush(len(tokens), true); err != nil {
		return nil, err
	}
	return items, nil
}

// near names the token a syntax error is reported at.
func near(last bool, tokens []Token, end int) string {
	if last {
		return "end of line"
	}
	return tokens[end].Value
}
