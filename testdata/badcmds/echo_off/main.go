// echo_off: disables echo on the terminal (like password prompts do) and
// exits without re-enabling it.  Typed characters become invisible, making
// the shell appear unresponsive even though it's processing input normally.
package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func main() {
	fd := int(os.Stdin.Fd())

	// Get current termios.
	var t syscall.Termios
	if _, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.TIOCGETA), uintptr(unsafe.Pointer(&t)),
		0, 0, 0,
	); errno != 0 {
		fmt.Fprintf(os.Stderr, "TIOCGETA: %v\n", errno)
		os.Exit(1)
	}

	// Disable echo.
	t.Lflag &^= syscall.ECHO

	if _, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.TIOCSETA), uintptr(unsafe.Pointer(&t)),
		0, 0, 0,
	); errno != 0 {
		fmt.Fprintf(os.Stderr, "TIOCSETA: %v\n", errno)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "echo disabled, exiting without restore")
	// Intentionally do NOT re-enable ECHO.
}
