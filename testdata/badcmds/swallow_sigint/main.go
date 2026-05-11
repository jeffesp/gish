// swallow_sigint: catches SIGINT and ignores it, then enters an infinite
// loop.  The shell sends SIGINT on Ctrl-C but the child never dies, so the
// shell blocks forever in cmd.Wait().
package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"
)

func main() {
	// Catch and ignore SIGINT.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		for range sig {
			// Swallow it.
		}
	}()

	fmt.Fprintln(os.Stdout, "ignoring SIGINT, looping forever")
	for {
		time.Sleep(time.Second)
	}
}
