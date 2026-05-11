// orphan_child: forks a grandchild process that holds stdin open and outlives
// the parent.  The grandchild continues to read from the terminal, racing
// with the shell for input and making it appear unresponsive.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	// Spawn a child that sleeps and holds stdin open.
	child := exec.Command("sh", "-c", "sleep 30 < /dev/stdin")
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start: %v\n", err)
		os.Exit(1)
	}
	// Parent exits immediately. The grandchild (sleep) is orphaned but
	// still has stdin open.
	fmt.Fprintln(os.Stdout, "parent exiting, orphan child holds stdin")
}
