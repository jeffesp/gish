package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
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

	// Any command other than `exit` re-arms the running-jobs warning, so an
	// earlier warning cannot authorise a later exit.
	if ctx.JobMgr != nil && !isExitCommand(exe) {
		ctx.JobMgr.ClearExitWarning()
	}

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
	} else if ctx.JobMgr != nil && ctx.Interactive() && runsAsJob(exe) {
		if _, err := ctx.JobMgr.StartForeground(exe, line, ctx); err != nil {
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

// runsAsJob reports whether an executable should run on its own PTY under the
// job manager.  Builtins mutate shell state and have to run in-process, but
// external commands and pipelines alike get a PTY so a full-screen program
// behaves the same wherever it appears.
func runsAsJob(exe Executable) bool {
	switch typed := exe.(type) {
	case *Pipeline:
		return true
	case *Command:
		_, builtin := Builtins[typed.Name()]
		return !builtin
	}
	return false
}

func isExitCommand(exe Executable) bool {
	command, ok := exe.(*Command)
	return ok && command.Name() == "exit"
}

// pushbackReader lets the shell hand back input it read but did not consume —
// the bytes typed after a Ctrl+Z — so the line editor picks them up next
// instead of them being dropped.
type pushbackReader struct {
	mu  sync.Mutex
	buf []byte
	src io.Reader
}

func newPushbackReader(src io.Reader) *pushbackReader {
	return &pushbackReader{src: src}
}

func (r *pushbackReader) Push(p []byte) {
	if len(p) == 0 {
		return
	}
	r.mu.Lock()
	r.buf = append(r.buf, p...)
	r.mu.Unlock()
}

func (r *pushbackReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		r.mu.Unlock()
		return n, nil
	}
	r.mu.Unlock()
	return r.src.Read(p)
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

	pushback := newPushbackReader(in)
	t := term.NewTerminal(readWriter{pushback, ctx.Out}, "gish> ")

	termCtx := &ExecCtx{
		In:       in,
		Out:      t,
		ErrOut:   t,
		SystemIO: ctx,
		JobMgr:   ctx.JobMgr,
		Pushback: pushback.Push,
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
			// Ctrl+D leaves through the same gate as `exit`, so it cannot kill
			// running jobs without the same warning.
			if errors.Is(err, io.EOF) && termCtx.JobMgr != nil {
				if running, warn := termCtx.JobMgr.WarnBeforeExit(); warn {
					fmt.Fprintf(termCtx.ErrOut, "gish: %d running jobs. Press Ctrl+D again to force.\n", running)
					continue
				}
			}
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
