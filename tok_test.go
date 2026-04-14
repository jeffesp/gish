package main

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		input     string
		wantVals  []string
		wantKinds []TokenKind
		wantErr   bool
	}{
		{"echo 'hello world'", []string{"echo", "hello world"}, []TokenKind{TokenWord, TokenSingleQuoted}, false},
		{`echo "hello world"`, []string{"echo", "hello world"}, []TokenKind{TokenWord, TokenDoubleQuoted}, false},
		{"echo '$HOME'", []string{"echo", "$HOME"}, []TokenKind{TokenWord, TokenSingleQuoted}, false},
		{"echo $HOME", []string{"echo", "$HOME"}, []TokenKind{TokenWord, TokenWord}, false},
		{"echo unclosed'quote", nil, nil, true},
		{"foo'bar'", []string{"foobar"}, []TokenKind{TokenWord}, false},
	}
	for _, c := range cases {
		toks, err := tokenize(c.input)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected error", c.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.input, err)
			continue
		}
		if len(toks) != len(c.wantVals) {
			t.Errorf("%q: got %d tokens, want %d", c.input, len(toks), len(c.wantVals))
			continue
		}
		for i, tok := range toks {
			if tok.Value != c.wantVals[i] {
				t.Errorf("%q token[%d]: value=%q want=%q", c.input, i, tok.Value, c.wantVals[i])
			}
			if tok.Kind != c.wantKinds[i] {
				t.Errorf("%q token[%d]: kind=%d want=%d", c.input, i, tok.Kind, c.wantKinds[i])
			}
		}
	}
}
