//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// watchWinch re-applies the terminal size every time the window is resized.
func watchWinch(update func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)
	defer close(ch)
	go func() {
		for range ch {
			update()
		}
	}()
}
