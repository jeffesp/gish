package main

import (
	"syscall"
	"unsafe"
)

// childStopped blocks until the child pid stops or exits and reports
// which. WNOWAIT leaves the child unreaped so exec.Cmd.Wait still collects
// its exit status.
//
// x/sys/unix has no Waitid on darwin and wait4 ignores WNOWAIT there (it
// reaps an exited child), so this calls waitid directly. The values below
// are from <sys/wait.h> and <sys/signal.h>; si_code is the third int32 of
// siginfo_t.
func childStopped(pid int) (bool, error) {
	const (
		pPID       = 1
		wExited    = 0x4
		wStopped   = 0x8
		wNoWait    = 0x20
		cldStopped = 5
	)
	var info [128]byte // siginfo_t is 104 bytes
	_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, pPID, uintptr(pid),
		uintptr(unsafe.Pointer(&info[0])), wExited|wStopped|wNoWait, 0, 0)
	if errno != 0 {
		return false, errno
	}
	return *(*int32)(unsafe.Pointer(&info[8])) == cldStopped, nil
}
