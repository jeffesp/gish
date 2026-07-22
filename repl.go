package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
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
	if ctx.JobMgr == nil {
		ctx.JobMgr = NewJobManager()
	}
	defer ctx.JobMgr.Shutdown()
	if f, ok := ctx.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
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
	tokens = expandVars(tokens)
	tokens = expandGlobs(tokens)
	tokens, err = expandAliases(tokens)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return
	}

	exe, background, err := parseTokens(tokens, line)
	if err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		return
	}

	dir, _ := os.Getwd()
	start := time.Now()
	code := 0

	if background {
		if ctx.JobMgr == nil {
			fmt.Fprintln(ctx.ErrOut, "gish: job control is unavailable")
			code = 1
		} else {
			command := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "&"))
			job, err := ctx.JobMgr.StartBackground(exe, command, ctx)
			if err != nil {
				fmt.Fprintf(ctx.ErrOut, "%v\n", err)
				code = exitCode(err)
			} else {
				fmt.Fprintf(ctx.Out, "[%d] started: %s\n", job.ID, job.Command)
			}
		}
	} else if command, ok := exe.(*Command); ok && ctx.JobMgr != nil {
		if _, builtin := Builtins[command.Name()]; !builtin {
			_, err := ctx.JobMgr.StartForeground(exe, line, ctx)
			if err != nil {
				fmt.Fprintf(ctx.ErrOut, "%v\n", err)
				code = exitCode(err)
			}
		} else if err := exe.Exec(ctx); err != nil {
			fmt.Fprintf(ctx.ErrOut, "%v\n", err)
			code = exitCode(err)
		}
	} else if err := exe.Exec(ctx); err != nil {
		fmt.Fprintf(ctx.ErrOut, "%v\n", err)
		code = exitCode(err)
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
	notifyCompletedJobs(ctx)
}

func notifyCompletedJobs(ctx *ExecCtx) {
	if ctx.JobMgr == nil {
		return
	}
	for _, job := range ctx.JobMgr.Reap() {
		snapshot := job.Snapshot()
		fmt.Fprintf(ctx.Out, "[%d] done (exit %d): %s\n", snapshot.ID, snapshot.ExitCode, snapshot.Command)
	}
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
		In:       in,
		Out:      t,
		ErrOut:   t,
		SystemIO: ctx,
		JobMgr:   ctx.JobMgr,
		RestoreTerm: func() func() {
			term.Restore(fd, origState)
			return func() { term.MakeRaw(fd) } //nolint:errcheck
		},
	}

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
				termCtx.JobMgr.ResizeForeground(w, h)
			}
		}
	}()

	t.History = newTermHistory()

	InitScripting(termCtx)

	for {
		notifyCompletedJobs(termCtx)
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
	InitScripting(ctx)
	scanner := bufio.NewScanner(ctx.In)
	for {
		notifyCompletedJobs(ctx)
		fmt.Fprint(ctx.Out, JSPrompt())
		if !scanner.Scan() {
			break
		}

		if line := strings.TrimSpace(scanner.Text()); line != "" {
			execLine(line, ctx)
		}

	}
}
