# Things that misbehave

Programs in this directory do some of the following to misbhave and then exit. The terminal needs to be able to recover from these as they will all happen to someone, somewhere. They will be run in a test harness and the terminal state will be checked before and after. For most of them. Some of these failure modes are not about term state, but system state and I will do my best to handle that as well.

## Terminal Attribute Corruption

- Program changes terminal settings and crashes before restoring them. E.g., a program calls tcsetattr() to disable echo or enable raw mode, then gets killed by SIGKILL (which can't be caught, so no cleanup handler runs).
- Program modifies termios flags gish doesn't know about. Your RestoreTerm saves/restores the full termios state, but only the state at the time MakeRaw was called. If a child modifies settings that were already non-default, restoration goes back to gish's raw-mode state, not the original baseline.

## Signal-Related Exits

- SIGKILL / SIGSTOP — unkillable signals where the child gets no chance to clean up. Any terminal state it changed stays changed.
- Child process catches SIGINT but doesn't exit. Gish sends os.Interrupt to active commands (signals.go:36-43), but if the child swallows it (e.g., a Python REPL that catches KeyboardInterrupt), gish's REPL may loop back before the child is actually done.
- Child dies from a signal that produces a core dump (SIGQUIT, SIGABRT, SIGSEGV). The cmd.Wait() returns an error, but the terminal state may be inconsistent depending on what the child was doing when it crashed.

## File Descriptor / Pipe State Issues

- Child process doesn't close stdin (or a dup'd copy of it). If a background child inherits the stdin fd and holds it open, the next read() on stdin in gish may block or return unexpected data.
- Orphaned grandchild processes. If the child forks a subprocess that inherits the terminal fd and outlives the child, it can race with gish for terminal input.
- Broken pipe in pipeline stages. Your pipeline code (command.go:77-121) only reports errors from the last stage. If an intermediate stage dies with the pipe still open, the read-side may block indefinitely — this is noted in your TODO.

## Buffered / Pending I/O

- Pending output in the terminal driver. If a child writes partial escape sequences (e.g., half of \e[2J) and then exits, the terminal driver may be waiting for the rest of the sequence, making it look hung until the next write completes the sequence or resets it.
- Stale input in the stdin buffer. If a child reads stdin in cooked mode with line buffering, characters typed during execution may sit in the kernel's line discipline buffer. When gish re-enters raw mode, those characters may be lost or delivered unexpectedly.

## Terminal Mode Restoration Race

- The defer reenter() pattern in command.go:66-69 restores raw mode after a command exits. If cmd.Run() returns but the child's output hasn't fully flushed to the terminal yet (e.g., the child's stdout is block-buffered), gish switches back to raw mode while output is still arriving — causing garbled display.
- In pipelines, RestoreTerm is called once for the whole pipeline (command.go:78-81), but stages exit asynchronously via the WaitGroup. The reenter() fires after the last Wait() returns, but a slow-dying process in an earlier stage could still be writing.

## Escape Sequence / Alternate Screen Issues

- Program enters alternate screen buffer (\e[?1049h) and crashes without restoring it (\e[?1049l). The terminal stays on the empty alternate screen. Programs like less, vim, htop do this.
- Program enables mouse reporting (\e[?1000h) and doesn't disable it. Every mouse event then generates escape sequences that gish reads as garbage input.
- Program changes the cursor visibility, color, or scrolling region and crashes before resetting.
- Bracketed paste mode (\e[?2004h) left enabled — typed text then gets wrapped in escape sequences gish may not expect.

## Process Group / Job Control Issues

- Child puts itself in a different process group (setpgid). Then signals sent by gish to the child's PID may not reach the child's subprocesses, leaving them orphaned and holding the terminal.
- Child tries to do its own job control (calls tcsetpgrp()), taking control of the terminal away from gish. If it exits without restoring the foreground process group, gish loses the ability to read from the terminal.
