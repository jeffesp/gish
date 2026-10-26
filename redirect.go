package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type RedirectKind int

const (
	RedirectOut       RedirectKind = iota // >   out>
	RedirectAppend                        // >>  out>>
	RedirectErr                           // 2>  err>
	RedirectErrAppend                     // 2>> err>>
	RedirectAll                           // &>  out+err>
	RedirectAllAppend                     // &>> out+err>>
	RedirectIn                            // <   in<
)

// Redirect sends or reads one stream of a command to or from a file.
type Redirect struct {
	Kind RedirectKind
	File string
}

// redirectOps lists every operator spelling, longest first within each
// family so ">>" wins over ">". Named spellings are readable aliases of the
// POSIX ones and map to the same canonical form.
var redirectOps = []struct{ spelling, canonical string }{
	{"out+err>>", "&>>"}, {"out+err>", "&>"},
	{"out>>", ">>"}, {"out>", ">"},
	{"err>>", "2>>"}, {"err>", "2>"},
	{"in<", "<"},
	{"&>>", "&>>"}, {"&>", "&>"},
	{"2>>", "2>>"}, {"2>", "2>"},
	{">>", ">>"}, {">", ">"},
	{"<", "<"},
}

var redirectKinds = map[string]RedirectKind{
	">": RedirectOut, ">>": RedirectAppend,
	"2>": RedirectErr, "2>>": RedirectErrAppend,
	"&>": RedirectAll, "&>>": RedirectAllAppend,
	"<": RedirectIn,
}

// redirectOp returns the canonical operator at the start of s and how many
// bytes it spans, or ("", 0) if s does not start with one.
func redirectOp(s string) (string, int) {
	for _, op := range redirectOps {
		if strings.HasPrefix(s, op.spelling) {
			return op.canonical, len(op.spelling)
		}
	}
	return "", 0
}

func redirectLen(s string) int {
	_, n := redirectOp(s)
	return n
}

// extractRedirects removes redirection operators and their file names from
// tokens. It returns the remaining tokens and the redirects for each
// pipeline stage, keyed by stage index (stages are separated by | and |&).
func extractRedirects(tokens []Token) ([]Token, map[int][]Redirect, error) {
	var out []Token
	redirects := map[int][]Redirect{}
	stage := 0
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t.Kind == TokenPipe || t.Kind == TokenMergePipe:
			stage++
			out = append(out, t)
		case t.Kind == TokenRedirect:
			if i+1 >= len(tokens) {
				return nil, nil, fmt.Errorf("syntax error: %s needs a file name", t.Value)
			}
			next := tokens[i+1]
			if next.Kind != TokenWord && next.Kind != TokenSingleQuoted && next.Kind != TokenDoubleQuoted {
				return nil, nil, fmt.Errorf("syntax error: %s needs a file name, found %q", t.Value, next.Value)
			}
			redirects[stage] = append(redirects[stage], Redirect{Kind: redirectKinds[t.Value], File: next.Value})
			i++
		default:
			out = append(out, t)
		}
	}
	return out, redirects, nil
}

// attachRedirects stores each stage's redirects on its Command.
func attachRedirects(exe Executable, redirects map[int][]Redirect) {
	switch e := exe.(type) {
	case *Command:
		e.Redirects = redirects[0]
	case *Pipeline:
		for i, c := range e.Stages {
			c.Redirects = redirects[i]
		}
	}
}

// applyRedirects opens the redirect targets in order (so for the same
// stream the last one wins) and returns a copy of ctx using them, plus a
// function that closes the files. If ctx has SystemIO, the copy's SystemIO
// is redirected too, since external commands use it in preference to ctx.
func applyRedirects(ctx *ExecCtx, redirects []Redirect) (*ExecCtx, func(), error) {
	if len(redirects) == 0 {
		return ctx, func() {}, nil
	}
	rctx := *ctx
	if ctx.SystemIO != nil {
		sys := *ctx.SystemIO
		rctx.SystemIO = &sys
	}
	var files []*os.File
	cleanup := func() {
		for _, f := range files {
			f.Close() //nolint:errcheck
		}
	}
	setIn := func(r io.Reader) {
		rctx.In = r
		if rctx.SystemIO != nil {
			rctx.SystemIO.In = r
		}
	}
	setOut := func(w io.Writer) {
		rctx.Out = w
		if rctx.SystemIO != nil {
			rctx.SystemIO.Out = w
		}
	}
	setErr := func(w io.Writer) {
		rctx.ErrOut = w
		if rctx.SystemIO != nil {
			rctx.SystemIO.ErrOut = w
		}
	}
	const appendFlags = os.O_APPEND | os.O_CREATE | os.O_WRONLY
	const truncFlags = os.O_TRUNC | os.O_CREATE | os.O_WRONLY
	for _, r := range redirects {
		flags := truncFlags
		switch r.Kind {
		case RedirectAppend, RedirectErrAppend, RedirectAllAppend:
			flags = appendFlags
		}
		if r.Kind == RedirectIn {
			f, err := os.Open(r.File)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			files = append(files, f)
			setIn(f)
			continue
		}
		f, err := os.OpenFile(r.File, flags, 0o644)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		files = append(files, f)
		switch r.Kind {
		case RedirectOut, RedirectAppend:
			setOut(f)
		case RedirectErr, RedirectErrAppend:
			setErr(f)
		default:
			setOut(f)
			setErr(f)
		}
	}
	return &rctx, cleanup, nil
}
