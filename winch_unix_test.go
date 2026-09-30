//go:build !windows

package main

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestWatchWinch sends the process real SIGWINCH signals and checks the
// update callback fires for each one. This guards against a regression
// where watchWinch deferred signal.Stop/close right after launching its
// goroutine — since watchWinch itself returns immediately, those defers
// ran essentially right away, tearing the watch down before it could
// ever see a real resize.
func TestWatchWinch(t *testing.T) {
	var got atomic.Int32
	watchWinch(func() { got.Add(1) })

	for want := int32(1); want <= 3; want++ {
		if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
			t.Fatalf("Kill: %v", err)
		}
		deadline := time.Now().Add(time.Second)
		for got.Load() < want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if got.Load() < want {
			t.Fatalf("update called %d times after signal #%d, want at least %d", got.Load(), want, want)
		}
	}
}
