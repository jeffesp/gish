// bracketed_paste: enables bracketed paste mode and exits without disabling
// it.  Pasted text will be wrapped in \x1b[200~ ... \x1b[201~ sequences that
// the shell may not expect, causing garbled or missing input.
package main

import (
	"fmt"
	"os"
)

func main() {
	// Enable bracketed paste mode.
	fmt.Fprint(os.Stdout, "\x1b[?2004h")
	fmt.Fprintln(os.Stdout, "bracketed paste enabled, exiting without disable")
	// Intentionally do NOT send \x1b[?2004l.
}
