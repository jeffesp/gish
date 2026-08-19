package main

import (
	"os"
	"slices"
	"testing"
)

// tokensEqual compares tokens field-wise; Token holds a slice, so it is
// not comparable and cannot be passed to slices.Equal.
func tokensEqual(a, b []Token) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Value != b[i].Value || !slices.Equal(a[i].Escapes, b[i].Escapes) {
			return false
		}
	}
	return true
}

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
		{"cat file.out | grep foo", []string{"cat", "file.out", "|", "grep", "foo"}, []TokenKind{TokenWord, TokenWord, TokenPipe, TokenWord, TokenWord}, false},
		{"cat file.out |& grep foo", []string{"cat", "file.out", "|&", "grep", "foo"}, []TokenKind{TokenWord, TokenWord, TokenMergePipe, TokenWord, TokenWord}, false},
		{"cat file.out|grep foo", []string{"cat", "file.out", "|", "grep", "foo"}, []TokenKind{TokenWord, TokenWord, TokenPipe, TokenWord, TokenWord}, false},
		{"cat file.out|&grep foo", []string{"cat", "file.out", "|&", "grep", "foo"}, []TokenKind{TokenWord, TokenWord, TokenMergePipe, TokenWord, TokenWord}, false},
		{"sleep 100 &", []string{"sleep", "100", "&"}, []TokenKind{TokenWord, TokenWord, TokenBackground}, false},
		{"a\\ b", []string{"a b"}, []TokenKind{TokenWord}, false},
		{`a\|b`, []string{"a|b"}, []TokenKind{TokenWord}, false},
		{"\"abc\\\"", nil, nil, true},
		{"\"abc\\", nil, nil, true},
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

func TestExpandTokens(t *testing.T) {
	os.Setenv("GISH_EXP_TEST", "hello")
	defer os.Unsetenv("GISH_EXP_TEST")

	cases := []struct {
		name  string
		input Token
		want  string
	}{
		{"bare var", Token{Kind: TokenWord, Value: "$GISH_EXP_TEST"}, "hello"},
		{"braced var", Token{Kind: TokenWord, Value: "${GISH_EXP_TEST}"}, "hello"},
		{"embedded var", Token{Kind: TokenWord, Value: "say$GISH_EXP_TEST"}, "sayhello"},
		{"double quoted", Token{Kind: TokenDoubleQuoted, Value: "$GISH_EXP_TEST"}, "hello"},
		{"single quoted no expand", Token{Kind: TokenSingleQuoted, Value: "$GISH_EXP_TEST"}, "$GISH_EXP_TEST"},
		{"no var", Token{Kind: TokenWord, Value: "plain"}, "plain"},
		{"unset var", Token{Kind: TokenWord, Value: "$GISH_UNSET_NONEXISTENT"}, ""},
		{"escaped var stays literal", Token{Kind: TokenWord, Value: "$GISH_EXP_TEST", Escapes: []int{0}}, "$GISH_EXP_TEST"},
		{"escaped braced var stays literal", Token{Kind: TokenWord, Value: "${GISH_EXP_TEST}", Escapes: []int{0}}, "${GISH_EXP_TEST}"},
		{"escaped embedded var stays literal", Token{Kind: TokenWord, Value: "say$GISH_EXP_TEST", Escapes: []int{3}}, "say$GISH_EXP_TEST"},
		{"partial escape leaves rest expandable", Token{Kind: TokenWord, Value: "a$X$GISH_EXP_TEST", Escapes: []int{1}}, "a$Xhello"},
		{"escaped dollar in double quotes", Token{Kind: TokenDoubleQuoted, Value: "$GISH_EXP_TEST", Escapes: []int{0}}, "$GISH_EXP_TEST"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandToken(c.input)
			if got.Value != c.want {
				t.Errorf("expandToken(%v): got %q want %q", c.input, got.Value, c.want)
			}
		})
	}
}

func TestGlobTokens(t *testing.T) {
	temp, err := os.MkdirTemp("", "gish-glob-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(temp)
	origDir, _ := os.Getwd()
	os.Chdir(temp)
	defer os.Chdir(origDir)

	os.Create("README.md")
	os.Create("TODO.md")
	os.Create(".hidden.md")

	cases := []struct {
		name  string
		input []Token
		want  []Token
	}{
		{"matches files", []Token{{Kind: TokenWord, Value: "*.md"}}, []Token{{Kind: TokenWord, Value: "README.md"}, {Kind: TokenWord, Value: "TODO.md"}}},
		{"excludes dotfiles by default", []Token{{Kind: TokenWord, Value: "*.md"}}, []Token{{Kind: TokenWord, Value: "README.md"}, {Kind: TokenWord, Value: "TODO.md"}}},
		{"dot pattern matches dotfiles", []Token{{Kind: TokenWord, Value: ".*.md"}}, []Token{{Kind: TokenWord, Value: ".hidden.md"}}},
		{"single quote string does not glob", []Token{{Kind: TokenSingleQuoted, Value: "'*.md'"}}, []Token{{Kind: TokenSingleQuoted, Value: "'*.md'"}}},
		{"double quote string does not glob", []Token{{Kind: TokenDoubleQuoted, Value: "\"*.md\""}}, []Token{{Kind: TokenDoubleQuoted, Value: "\"*.md\""}}},
		{"not matching does not glob", []Token{{Kind: TokenWord, Value: "*.xyz"}}, []Token{{Kind: TokenWord, Value: "*.xyz"}}},
		{"escaped pattern does not glob", []Token{{Kind: TokenWord, Value: "*.md", Escapes: []int{0}}}, []Token{{Kind: TokenWord, Value: "*.md", Escapes: []int{0}}}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandGlobs(c.input)
			if !tokensEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestTokenizeEscapes(t *testing.T) {
	cases := []struct {
		input string
		want  []Token
	}{
		{"a\\ b", []Token{{Kind: TokenWord, Value: "a b", Escapes: []int{1}}}},
		{"\\$HOME", []Token{{Kind: TokenWord, Value: "$HOME", Escapes: []int{0}}}},
		{"a\\$b", []Token{{Kind: TokenWord, Value: "a$b", Escapes: []int{1}}}},
		{"a\\|b", []Token{{Kind: TokenWord, Value: "a|b", Escapes: []int{1}}}},
		{"a\\|&b", []Token{{Kind: TokenWord, Value: "a|&b", Escapes: []int{1}}}},
		{"a\\&", []Token{{Kind: TokenWord, Value: "a&", Escapes: []int{1}}}},
		{"a\\\\", []Token{{Kind: TokenWord, Value: "a\\", Escapes: []int{1}}}},
		{"\\\"abc", []Token{{Kind: TokenWord, Value: "\"abc", Escapes: []int{0}}}},
		{"'a\\b'", []Token{{Kind: TokenSingleQuoted, Value: "a\\b"}}},
		{"\"\\$HOME\"", []Token{{Kind: TokenDoubleQuoted, Value: "$HOME", Escapes: []int{0}}}},
		{"\"a\\\"b\"", []Token{{Kind: TokenDoubleQuoted, Value: "a\"b", Escapes: []int{1}}}},
		{"\"a\\\\b\"", []Token{{Kind: TokenDoubleQuoted, Value: "a\\b", Escapes: []int{1}}}},
		{"\"a\\nb\"", []Token{{Kind: TokenDoubleQuoted, Value: "a\\nb"}}},
		{"a\\", []Token{{Kind: TokenWord, Value: "a\\"}}},
		{"a\\ b|c", []Token{{Kind: TokenWord, Value: "a b", Escapes: []int{1}}, {Kind: TokenPipe, Value: "|"}, {Kind: TokenWord, Value: "c"}}},
	}
	for _, c := range cases {
		toks, err := tokenize(c.input)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.input, err)
			continue
		}
		if !tokensEqual(toks, c.want) {
			t.Errorf("%q: got %v, want %v", c.input, toks, c.want)
		}
	}
}
