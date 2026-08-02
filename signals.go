package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

var (
	activeCmds  = make(map[*exec.Cmd]struct{})
	activeCmdMu sync.Mutex
)

func SetCurrentCmd(cmd *exec.Cmd) func() {
	activeCmdMu.Lock()
	activeCmds[cmd] = struct{}{}
	activeCmdMu.Unlock()
	return func() {
		activeCmdMu.Lock()
		delete(activeCmds, cmd)
		activeCmdMu.Unlock()
	}
}

func SetupSignals(out io.Writer) {
	// The shell must not suspend when a stray SIGTSTP reaches it.  A handler
	// that drops the signal does that without ever setting SIG_IGN on the
	// process: POSIX propagates an ignored disposition across exec, so relying
	// on signal.Ignore here would leave children unstoppable on any runtime
	// that does not scrub dispositions before exec.  Go's os/exec does scrub
	// them, so this is belt-and-braces rather than a fix — see
	// testdata/badcmds/tstp_probe, which asserts children get SIG_DFL.
	tstpCh := make(chan os.Signal, 1)
	signal.Notify(tstpCh, syscall.SIGTSTP)
	go func() {
		for range tstpCh {
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	go func() {
		for sig := range sigCh {
			switch sig {
			case os.Interrupt:
				activeCmdMu.Lock()
				for cmd := range activeCmds {
					if cmd != nil && cmd.Process != nil {
						cmd.Process.Signal(os.Interrupt)
					}
				}
				activeCmdMu.Unlock()
			case syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP:
				fmt.Fprintln(out)
				os.Exit(0)
			}
		}
	}()
}
