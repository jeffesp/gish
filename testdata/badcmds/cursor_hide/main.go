// cursor_hide: hides the cursor and exits without restoring it.
// The terminal cursor becomes invisible, making it look like the shell is
// unresponsive even though it's actually still running and accepting input.
package main

import (
	"fmt"
	"os"
)

func main() {
	// Hide cursor.
	fmt.Fprint(os.Stdout, "\x1b[?25l")
	fmt.Fprintln(os.Stdout, "cursor hidden, exiting without restore")
	// Intentionally do NOT send \x1b[?25h.
}
