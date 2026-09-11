package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type Executable interface {
	Exec(ctx *ExecCtx) error
}

type Command struct {
	Tokens []Token
	Line   string
}

func (cmd *Command) Args() []Token {
	if len(cmd.Tokens) < 2 {
		return nil
	}
	return cmd.Tokens[1:]
}

func (cmd *Command) Name() string {
	if len(cmd.Tokens) == 0 {
		return ""
	}
	return cmd.Tokens[0].Value
}

// Start launches the command and returns two functions:
//
//   - wait blocks until the command finishes and returns its result
//   - kill terminates a still-running command; it is a no-op for builtins
//     (their goroutines cannot be interrupted) and for stages that have
//     already finished
func (c *Command) Start(ctx *ExecCtx) (wait func() error, kill func()) {
	if fn, ok := Builtins[c.Name()]; ok {
		done := make(chan error, 1)
		go func() { done <- fn(c, ctx) }()
		return func() error { return <-done }, func() {}
	}

	cmd := exec.Command(c.Name(), tokenValues(c.Args())...)
	setNewProcessGroup(cmd)
	clearCmd := SetCurrentCmd(cmd)

	cmd.Stdin = ctx.In
	cmd.Stdout = ctx.Out
	cmd.Stderr = ctx.ErrOut
	if err := cmd.Start(); err != nil {
		clearCmd()
		return func() error { return err }, func() {}
	}
	return func() error {
			defer clearCmd()
			return cmd.Wait()
		}, func() {
			// Kills the whole process group, not just cmd's own pid: a
			// stage like "sh -c '...'" may fork children that inherit
			// this stage's pipe fds, and killing only the shell leaves
			// them running and holding those fds open.
			killProcessGroup(cmd)
		}
}

func (c *Command) Exec(ctx *ExecCtx) error {
	if fn, ok := Builtins[c.Name()]; ok {
		return fn(c, ctx)
	}
	cmd := exec.Command(c.Name(), tokenValues(c.Args())...)
	clearCmd := SetCurrentCmd(cmd)
	defer clearCmd()

	ctx.WireCmd(cmd)
	if ctx.RestoreTerm != nil {
		reenter := ctx.RestoreTerm()
		defer reenter()
	}
	return cmd.Run()
}

type Pipeline struct {
	Stages   []*Command
	MergeErr []bool
}

// pipelineStageError identifies which pipeline stage failed. It unwraps to
// the stage's own error, so exitCode() still extracts the process exit code
// (via errors.As) for $?.
type pipelineStageError struct {
	stage   int
	command string
	cause   error
}

func (e *pipelineStageError) Error() string {
	return fmt.Sprintf("pipeline stage %d (%s): %v", e.stage, e.command, e.cause)
}

func (e *pipelineStageError) Unwrap() error {
	return e.cause
}

type pipelineStage struct {
	wait func() error
	kill func()
}

func (p *Pipeline) Exec(ctx *ExecCtx) error {
	if ctx.RestoreTerm != nil {
		reenter := ctx.RestoreTerm()
		defer reenter()
	}

	stages := make([]pipelineStage, len(p.Stages))
	var nextIn io.Reader = ctx.In
	var prevPR *os.File // read end of the previous pipe — this stage's stdin
	for i, cmd := range p.Stages {
		localCtx := &ExecCtx{
			In:     nextIn,
			ErrOut: ctx.ErrOut,
		}
		var pr, pw *os.File
		if i == len(p.Stages)-1 {
			localCtx.Out = ctx.Out
		} else {
			var err error
			pr, pw, err = os.Pipe()
			if err != nil {
				return fmt.Errorf("unable to create pipe: %v", err)
			}
			localCtx.Out = pw
			// if merging err+out assign to the same writer
			if len(p.MergeErr) > i && p.MergeErr[i] {
				localCtx.ErrOut = pw
			}
			nextIn = pr
		}

		wait, kill := cmd.Start(localCtx)
		if pw != nil || prevPR != nil {
			// Close the pipe ends the parent still holds once the
			// stage that owns them finishes: pw (this stage's stdout)
			// so the next stage receives EOF, and prevPR (this stage's
			// stdin) so the read end of the previous pipe isn't held
			// open for the rest of the session. (Capture wait in inner
			// first — the closure must not refer to itself.)
			closers := make([]io.Closer, 0, 2)
			if pw != nil {
				closers = append(closers, pw)
			}
			if prevPR != nil {
				closers = append(closers, prevPR)
			}
			inner := wait
			wait = func() error {
				err := inner()
				for _, c := range closers {
					c.Close() //nolint:errcheck
				}
				return err
			}
		}
		prevPR = pr
		stages[i] = pipelineStage{wait: wait, kill: kill}
	}

	// All stages run concurrently (required — the pipe buffer is finite).
	// The first failure kills every still-running stage, so the pipeline
	// doesn't drain on a failed early stage and a stage blocked on input
	// (e.g. `cat | badcmd`, where `cat` waits for the terminal) can't
	// hold the pipeline open. Builtin stages can't be killed — their
	// goroutines aren't interruptible — but they exit on EOF once the
	// stage feeding them dies.
	errs := make([]error, len(stages))
	done := make(chan int, len(stages))
	var (
		mu     sync.Mutex
		alive  = make([]bool, len(stages))
		killed = make([]bool, len(stages))
	)
	for i := range alive {
		alive[i] = true
	}
	for i := range stages {
		go func(i int) {
			errs[i] = stages[i].wait()
			mu.Lock()
			alive[i] = false
			if errs[i] != nil {
				for j := range alive {
					if alive[j] {
						alive[j] = false
						killed[j] = true
						stages[j].kill()
					}
				}
			}
			mu.Unlock()
			done <- i
		}(i)
	}
	for range stages {
		<-done
	}

	// Surface every stage that failed on its own. Stages we killed after
	// the first failure only report "killed" as a consequence of that
	// failure — including them would add noise and would skew $? (a
	// signal-killed process reports exit code -1).
	var failed []error
	for i, err := range errs {
		if err == nil || killed[i] {
			continue
		}
		failed = append(failed, &pipelineStageError{
			stage:   i,
			command: p.Stages[i].Name(),
			cause:   err,
		})
	}
	if len(failed) == 0 {
		return nil
	}
	if len(failed) == 1 {
		return failed[0]
	}
	return errors.Join(failed...)
}
