// scroll_region: sets the scrolling region to a small portion of the screen
// and exits without resetting it.  Output and scrolling are confined to the
// restricted region, making the shell appear broken.
package main

import (
	"fmt"
	"os"
)

func main() {
	// Set scroll region to rows 5-10 only.
	fmt.Fprint(os.Stdout, "\x1b[5;10r")
	// Move cursor into the scroll region.
	fmt.Fprint(os.Stdout, "\x1b[5;1H")
	fmt.Fprintln(os.Stdout, "scroll region restricted, exiting without reset")
	// Intentionally do NOT send \x1b[r to reset.
}
