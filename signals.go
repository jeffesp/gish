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
	signal.Ignore(syscall.SIGTSTP)
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
