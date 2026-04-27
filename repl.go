package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

type readWriter struct {
	io.Reader
	io.Writer
}

func RunREPL(ctx *ExecCtx) {
	if f, ok := ctx.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		runRawREPL(ctx)
		return
	}
	runScannerREPL(ctx)
}

func execLine(line string, ctx *ExecCtx) {
	if expanded, ok := expandHistory(line); ok {
		fmt.Fprintln(ctx.Out, expanded)
		line = expanded
	}

	tokens, err := tokenize(line)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return
	}
	tokens = expandVars(tokens)
	tokens = expandGlobs(tokens)
	tokens, err = expandAliases(tokens)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return
	}

	exe, err := parseTokens(tokens, line)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return
	}

	dir, _ := os.Getwd()
	start := time.Now()
	code := 0

	if err := exe.Exec(ctx); err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		code = exitCode(err)
	}

	appendHistory(HistoryEntry{
		Command:   line,
		Dir:       dir,
		ExitCode:  code,
		StartTime: start,
		EndTime:   time.Now(),
		SessionID: sessionID,
	})
}

func runRawREPL(ctx *ExecCtx) {
	in := ctx.In.(*os.File)
	fd := int(in.Fd())
	origState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gish: failed to set raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(fd, origState)

	t := term.NewTerminal(readWriter{in, ctx.Out}, "gish> ")

	termCtx := &ExecCtx{
		In:     in,
		Out:    t,
		ErrOut: t,
		RestoreTerm: func() func() {
			term.Restore(fd, origState)
			return func() { term.MakeRaw(fd) } //nolint:errcheck
		},
		RunCmd: func(cmd *exec.Cmd) error {
			cmd.Stdin = ctx.In
			cmd.Stdout = ctx.Out
			cmd.Stderr = ctx.ErrOut
			term.Restore(fd, origState)
			err := cmd.Run()
			term.MakeRaw(fd) //nolint:errcheck
			return err
		}}

	if w, h, err := term.GetSize(fd); err == nil {
		t.SetSize(w, h)
	}

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	defer close(winch)
	go func() {
		for range winch {
			if w, h, err := term.GetSize(fd); err == nil {
				t.SetSize(w, h)
			}
		}
	}()

	t.History = newTermHistory()

	InitScripting(termCtx)

	for {
		t.SetPrompt(JSPrompt())
		line, err := t.ReadLine()
		if err != nil {
			break
		}

		if line = strings.TrimSpace(line); line != "" {
			execLine(line, termCtx)
		}
	}
}

func runScannerREPL(ctx *ExecCtx) {
	ctx.RunCmd = func(cmd *exec.Cmd) error {
		cmd.Stdin = ctx.In
		cmd.Stdout = ctx.Out
		cmd.Stderr = ctx.ErrOut
		return cmd.Run()
	}
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
