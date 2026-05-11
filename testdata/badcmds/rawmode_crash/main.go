// rawmode_crash: enters raw mode on its own stdin and then exits without
// restoring terminal state.  This leaves the terminal in raw mode, which
// means no line buffering, no echo, and no signal generation from Ctrl-C.
package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

func main() {
	fd := int(os.Stdin.Fd())
	_, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "MakeRaw: %v\n", err)
		os.Exit(1)
	}
	// Intentionally do NOT restore. Simulates a crash after entering raw mode.
	fmt.Fprint(os.Stdout, "entered raw mode, exiting without restore\r\n")
}
