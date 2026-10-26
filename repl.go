package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	xterm "golang.org/x/term"

	"gish/internal/term"
)

type readWriter struct {
	io.Reader
	io.Writer
}

func RunREPL(ctx *ExecCtx) {
	if f, ok := ctx.In.(*os.File); ok && xterm.IsTerminal(int(f.Fd())) {
		runRawREPL(ctx)
		return
	}
	runScannerREPL(ctx)
}

func execLine(line string, ctx *ExecCtx) {
	skipHistory := false
	if expanded, ok := expandHistory(line); ok {
		fmt.Fprintln(ctx.Out, expanded)
		line = expanded
		skipHistory = true
	}

	tokens, err := tokenize(line)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return
	}
	items, err := splitList(tokens)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return
	}

	dir, _ := os.Getwd()
	start := time.Now()
	code := 0

	// Each item is expanded just before it runs, so $? and the working
	// directory reflect the items before it. An item skipped by && or ||
	// leaves the status of the last item that did run.
	for _, item := range items {
		if item.Op == TokenAnd && code != 0 || item.Op == TokenOr && code == 0 {
			continue
		}
		c, ok := execItem(item.Tokens, line, ctx)
		if !ok {
			return
		}
		code = c
	}

	if !skipHistory {
		appendHistory(HistoryEntry{
			Command:   line,
			Dir:       dir,
			ExitCode:  code,
			StartTime: start,
			EndTime:   time.Now(),
			SessionID: sessionID,
		})
	}
}

func runRawREPL(ctx *ExecCtx) {
	in := ctx.In.(*os.File)
	fd := int(in.Fd())
	origState, err := xterm.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gish: failed to set raw mode: %v\n", err)
		os.Exit(1)
	}
	defer xterm.Restore(fd, origState)

	t := term.NewTerminal(readWriter{in, ctx.Out}, "gish> ")
	t.AutoCompleteCallback = completeLine
	t.PreKeyCallback = func(string, int, rune) { clearListing() }
	completionOut = ctx.Out
	completionTerm = t

	termCtx := &ExecCtx{
		In:       in,
		Out:      t,
		ErrOut:   t,
		SystemIO: ctx,
		RestoreTerm: func() func() {
			xterm.Restore(fd, origState)
			return func() { xterm.MakeRaw(fd) } //nolint:errcheck
		},
	}

	if w, h, err := xterm.GetSize(fd); err == nil {
		t.SetSize(w, h)
		completionWidth = w
	}

	watchWinch(func() {
		if w, h, err := xterm.GetSize(fd); err == nil {
			t.SetSize(w, h)
			completionWidth = w
		}
	})

	t.History = newTermHistory()

	InitScripting(termCtx)

	for {
		t.SetPrompt(JSPrompt())
		line, err := t.ReadLine()
		lastListingLines = 0
		if err != nil {
			break
		}

		if line = strings.TrimSpace(line); line != "" {
			execLine(line, termCtx)
		}
	}
}

func runScannerREPL(ctx *ExecCtx) {
	InitScripting(ctx)
	scanner := bufio.NewScanner(ctx.In)
	for {
		fmt.Fprint(ctx.Out, JSPrompt())
		if !scanner.Scan() {
			break
		}

		if line := strings.TrimSpace(scanner.Text()); line != "" {
			execLine(line, ctx)
		}

	}
}

// execItem expands, parses and runs one command or pipeline of a command
// list and returns its exit code. ok is false if the item could not be
// built (a syntax error was reported), which abandons the rest of the line.
func execItem(tokens []Token, line string, ctx *ExecCtx) (code int, ok bool) {
	tokens = expandTildes(tokens)
	tokens = expandVars(tokens)
	tokens = expandGlobs(tokens)
	tokens, err := expandAliases(tokens)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return 0, false
	}

	tokens, redirects, err := extractRedirects(tokens)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return 0, false
	}
	if len(tokens) == 0 {
		fmt.Fprintln(ctx.ErrOut, "syntax error: redirection without a command")
		return 0, false
	}

	exe, _, err := parseTokens(tokens, line)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return 0, false
	}
	attachRedirects(exe, redirects)

	if err := exe.Exec(ctx); err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		code = exitCode(err)
	}
	recordExitCode(code)
	return code, true
}
