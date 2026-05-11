// partial_escape: writes an incomplete escape sequence and then exits.
// The terminal may buffer the partial sequence and wait for the rest,
// causing subsequent output to be eaten or misinterpreted.
package main

import "os"

func main() {
	// Write an incomplete CSI sequence: ESC [ but no terminator.
	// The terminal is now expecting the rest of the sequence.
	os.Stdout.Write([]byte("\x1b["))
}
