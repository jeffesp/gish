// alt_screen_crash: switches to the alternate screen buffer (like vim, less,
// htop do) and then exits without switching back.  The terminal remains on the
// empty alternate screen, making it look like the shell disappeared.
package main

import (
	"fmt"
	"os"
)

func main() {
	// Enter alternate screen buffer.
	fmt.Fprint(os.Stdout, "\x1b[?1049h")
	fmt.Fprint(os.Stdout, "you are on the alternate screen\r\n")
	// Intentionally do NOT send \x1b[?1049l to leave it.
}
