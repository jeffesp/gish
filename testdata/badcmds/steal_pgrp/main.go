// steal_pgrp: takes over the foreground process group of the terminal and
// then exits without restoring it.  The shell loses its foreground status,
// which means reads from the terminal return EIO and the shell appears hung.
package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func main() {
	fd := int(os.Stdin.Fd())

	// Create a new process group for ourselves.
	if err := syscall.Setpgid(0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "setpgid: %v\n", err)
		os.Exit(1)
	}

	// Grab the foreground process group on the controlling terminal.
	pgid := os.Getpid()
	if _, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&pgid)),
	); errno != 0 {
		fmt.Fprintf(os.Stderr, "TIOCSPGRP: %v\n", errno)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "stole foreground pgrp, exiting without restore")
	// Exit without restoring the original foreground process group.
}
