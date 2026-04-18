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

func execLine(line string, ctx *ExecCtx, runCmd func(*exec.Cmd) error) {
	if expanded, ok := expandHistory(line); ok {
		fmt.Fprintln(ctx.Out, expanded)
		line = expanded
	}

	tokens, err := tokenize(line)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "error: %v\n", err)
		return
	}
	tokens = expandTokens(tokens)
	ctx.Line = line
	ctx.Tokens = tokens

	dir, _ := os.Getwd()
	start := time.Now()
	code := 0

	if fn, ok := Builtins[ctx.Name()]; ok {
		if err := fn(ctx); err != nil {
			fmt.Fprintf(ctx.ErrOut, "error: %v\n", err)
			code = 1
		}
	} else {
		cmd := exec.Command(ctx.Name(), tokenValues(ctx.Args())...)
		SetCurrentCmd(cmd)
		if err := runCmd(cmd); err != nil {
			fmt.Fprintf(ctx.ErrOut, "error: %v\n", err)
			code = exitCode(err)
		}
		ClearCurrentCmd()
	}

	appendHistory(HistoryEntry{
		Command:   ctx.Line,
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
		runScannerREPL(ctx)
		return
	}
	defer term.Restore(fd, origState)

	t := term.NewTerminal(readWriter{in, ctx.Out}, "gish> ")

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

	termCtx := &ExecCtx{In: in, Out: t, ErrOut: t}
	InitScripting(termCtx)

	for {
		line, err := t.ReadLine()
		if err != nil {
			break
		}

		if line = strings.TrimSpace(line); line != "" {
			execLine(line, termCtx, func(cmd *exec.Cmd) error {
				cmd.Stdin = in
				cmd.Stdout = ctx.Out
				cmd.Stderr = ctx.ErrOut
				term.Restore(fd, origState)
				err := cmd.Run()
				term.MakeRaw(fd) //nolint:errcheck
				return err
			})
		}
	}
}

func runScannerREPL(ctx *ExecCtx) {
	InitScripting(ctx)
	scanner := bufio.NewScanner(ctx.In)
	for {
		fmt.Fprint(ctx.Out, "gish> ")
		if !scanner.Scan() {
			break
		}

		if line := strings.TrimSpace(scanner.Text()); line != "" {
			execLine(line, ctx, func(cmd *exec.Cmd) error {
				cmd.Stdin = ctx.In
				cmd.Stdout = ctx.Out
				cmd.Stderr = ctx.ErrOut
				return cmd.Run()
			})
		}

	}
}
