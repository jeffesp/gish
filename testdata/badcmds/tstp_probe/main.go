// tstp_probe: reports whether the shell leaked an ignored SIGTSTP disposition
// into it.  A SIG_IGN disposition survives exec, so a shell that calls
// signal.Ignore(SIGTSTP) to keep itself from suspending makes every child
// inherit SIG_IGN — which silently breaks job control inside anything it
// launches, such as a nested shell or a pager offering its own suspend.
//
// The disposition is queried rather than exercised: a job runs in its own
// session, so its process group is orphaned and the kernel discards SIGTSTP
// sent to it whatever the disposition happens to be.  The Go runtime preserves
// an inherited SIG_IGN, so signal.Ignored reports exactly what was handed down.
package main

import (
	"fmt"
	"os/signal"
	"syscall"
)

func main() {
	if signal.Ignored(syscall.SIGTSTP) {
		fmt.Print("SURVIVED-SIGTSTP: disposition was SIG_IGN\r\n")
		return
	}
	fmt.Print("INHERITED-SIGDFL: disposition was not ignored\r\n")
}
