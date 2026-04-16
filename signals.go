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
	activeCmd   *exec.Cmd
	activeCmdMu sync.Mutex
)

func SetCurrentCmd(cmd *exec.Cmd) {
	activeCmdMu.Lock()
	activeCmd = cmd
	activeCmdMu.Unlock()
}

func ClearCurrentCmd() {
	activeCmdMu.Lock()
	activeCmd = nil
	activeCmdMu.Unlock()
}

func SetupSignals(out io.Writer) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	go func() {
		for sig := range sigCh {
			switch sig {
			case os.Interrupt:
				activeCmdMu.Lock()
				cmd := activeCmd
				activeCmdMu.Unlock()
				if cmd != nil && cmd.Process != nil {
					cmd.Process.Signal(os.Interrupt)
				}
			case syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP:
				fmt.Fprintln(out)
				os.Exit(0)
			}
		}
	}()
}
