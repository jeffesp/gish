// alt_screen_hold: behaves like htop or vim — switches to the alternate
// screen, hides the cursor, enables mouse reporting, and keeps redrawing until
// it is killed.  Unlike alt_screen_crash it stays running, so it can be
// backgrounded with Ctrl+Z and brought back with fg.
//
// It repaints on SIGWINCH, which is how the shell asks a full-screen job to
// redraw itself instead of replaying buffered frames.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[?1000h")

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			fmt.Fprint(os.Stdout, "\x1b[H\x1b[2JREPAINT\r\n")
		}
	}()

	fmt.Fprint(os.Stdout, "HOLDING\r\n")
	for i := 0; ; i++ {
		fmt.Fprintf(os.Stdout, "\x1b[H\x1b[2JFRAME %d\r\n", i)
		time.Sleep(100 * time.Millisecond)
	}
}
