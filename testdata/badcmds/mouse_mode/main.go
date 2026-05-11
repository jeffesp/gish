// mouse_mode: enables xterm mouse reporting and then exits without disabling
// it.  Every subsequent mouse click/scroll in the terminal generates escape
// sequences that the shell reads as garbage input.
package main

import (
	"fmt"
	"os"
)

func main() {
	// Enable mouse button tracking (1000) and SGR extended mode (1006).
	fmt.Fprint(os.Stdout, "\x1b[?1000h\x1b[?1006h")
	fmt.Fprint(os.Stdout, "mouse reporting enabled, exiting without disable\r\n")
	// Intentionally do NOT send \x1b[?1000l\x1b[?1006l.
}
