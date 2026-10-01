package main

import "golang.org/x/sys/unix"

// cldStopped is CLD_STOPPED from <bits/siginfo-consts.h>; x/sys/unix lacks it.
const cldStopped = 5

// childStopped blocks until the child pid stops or exits and reports
// which. WNOWAIT leaves the child unreaped so exec.Cmd.Wait still collects
// its exit status.
func childStopped(pid int) (bool, error) {
	var info unix.Siginfo
	err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WSTOPPED|unix.WNOWAIT, nil)
	return err == nil && info.Code == cldStopped, err
}
