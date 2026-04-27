package main

import (
	"fmt"
	"strings"
)

var aliases = map[string]string{}

func setAlias(name, body string) error {
	if name == "" {
		return fmt.Errorf("alias name must not be empty")
	}
	if strings.ContainsAny(name, " \t|'\"") {
		return fmt.Errorf("alias `%q` contains invalid characters", name)
	}
	aliases[name] = body
	return nil
}

func unsetAlias(name string) {
	delete(aliases, name)
}

func getAlias(name string) (string, bool) {
	body, ok := aliases[name]
	return body, ok
}

// expandAliases replaces the first TokenWord at each command position (start
// of stream and after every TokenPipe) with the re-tokenized alias body.
// A name already expanded in this pass is not re-expanded, so an alias like
// `ls='ls --color'` terminates instead of looping.
func expandAliases(tokens []Token) ([]Token, error) {
	return expandAliasesWith(tokens, map[string]bool{})
}

func expandAliasesWith(tokens []Token, visited map[string]bool) ([]Token, error) {
	out := make([]Token, 0, len(tokens))
	atCmdStart := func() bool {
		if len(out) == 0 {
			return true
		}
		return out[len(out)-1].Kind == TokenPipe
	}
	for _, t := range tokens {
		if t.Kind != TokenWord || !atCmdStart() {
			out = append(out, t)
			continue
		}
		body, ok := getAlias(t.Value)
		if !ok || visited[t.Value] {
			out = append(out, t)
			continue
		}

		expanded, err := tokenize(body)
		if err != nil {
			return nil, fmt.Errorf("alias %q: %w", t.Value, err)
		}

		nextVisited := map[string]bool{t.Value: true}
		for k := range visited {
			nextVisited[k] = true
		}
		recursed, err := expandAliasesWith(expanded, nextVisited)
		if err != nil {
			return nil, err
		}
		out = append(out, recursed...)
	}
	return out, nil
}
