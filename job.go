package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type JobState int

const (
	JobRunning JobState = iota
	JobDone
)

type Job struct {
	ID             int
	Command        string
	Dir            string
	StartTime      time.Time
	State          JobState
	ExitCode       int
	ExitErr        error
	ptyMaster      *os.File
	cmd            *exec.Cmd
	output         *RingBuffer
	done           chan struct{}
	mu             sync.Mutex
	cmds           map[*exec.Cmd]struct{}
	reported       bool
	ioMu           sync.Mutex
	foreground     io.Writer
	totalBytes     uint64
	displayedBytes uint64
	term           *TerminalState
	pgid           int
}

type JobManager struct {
	mu         sync.Mutex
	jobs       map[int]*Job
	nextID     int
	fgJob      *Job
	bufferSize int
	exitWarned bool
}

func NewJobManager() *JobManager {
	return &JobManager{
		jobs:   make(map[int]*Job),
		nextID: 1,
	}
}

func (jm *JobManager) StartBackground(exe Executable, line string, ctx *ExecCtx) (*Job, error) {
	job, err := jm.startJob(exe, line, ctx)
	if err != nil {
		return nil, err
	}
	jm.registerJob(job)
	return job, nil
}

func (jm *JobManager) startJob(exe Executable, line string, ctx *ExecCtx) (*Job, error) {
	if exe == nil {
		return nil, fmt.Errorf("cannot start an empty job")
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	job := &Job{
		Command:   line,
		Dir:       dir,
		StartTime: time.Now(),
		State:     JobRunning,
		output:    NewRingBuffer(jm.bufferSize),
		done:      make(chan struct{}),
		cmds:      make(map[*exec.Cmd]struct{}),
		term:      NewTerminalState(),
	}
	switch typed := exe.(type) {
	case *Command:
		if _, builtin := Builtins[typed.Name()]; !builtin {
			if err := jm.startPTYCommand(job, typed); err != nil {
				return nil, err
			}
			return job, nil
		}
	case *Pipeline:
		if err := jm.startPTYPipeline(job, typed, ctx); err != nil {
			return nil, err
		}
		return job, nil
	}
	if err := jm.startBufferedExecutable(job, exe, ctx); err != nil {
		return nil, err
	}
	return job, nil
}

// procGroup gives a pipeline's first stage the job's PTY as its controlling
// terminal, so a program that opens /dev/tty for keyboard input works the same
// inside a pipeline as it does on its own.
//
// Only the first stage gets it.  Taking the controlling terminal requires
// becoming a session leader, and a later stage forked from the shell cannot
// then setpgid into that new session — the kernel returns EPERM.  The remaining
// stages stay in the shell's session and are signalled individually; they still
// have the PTY as stdout, which is what makes isatty and window-size queries
// behave.
type procGroup struct {
	mu       sync.Mutex
	leader   int
	onLeader func(int)
}

func (pg *procGroup) prepare(cmd *exec.Cmd) func() {
	pg.mu.Lock()
	if pg.leader == 0 {
		// Ctty 0 is the child's stdin, which the pipeline wires to the PTY.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	}
	pg.mu.Unlock()

	return func() {
		pg.mu.Lock()
		if pg.leader != 0 || cmd.Process == nil {
			pg.mu.Unlock()
			return
		}
		pg.leader = cmd.Process.Pid
		notify := pg.onLeader
		pg.mu.Unlock()
		if notify != nil {
			notify(cmd.Process.Pid)
		}
	}
}

func (jm *JobManager) registerJob(job *Job) {
	jm.mu.Lock()
	job.ID = jm.nextID
	jm.nextID++
	jm.jobs[job.ID] = job
	jm.mu.Unlock()
}

func (jm *JobManager) StartForeground(exe Executable, line string, ctx *ExecCtx) (*Job, error) {
	job, err := jm.startJob(exe, line, ctx)
	if err != nil {
		return nil, err
	}
	in := ctx.In
	if ctx.SystemIO != nil {
		in = ctx.SystemIO.In
	}
	if input, ok := in.(*os.File); ok {
		if width, height, err := term.GetSize(int(input.Fd())); err == nil {
			job.resize(width, height)
		}
	}
	backgrounded, err := jm.WaitForForeground(job, ctx)
	if backgrounded {
		jm.registerJob(job)
		fmt.Fprintf(ctx.Out, "[%d] backgrounded: %s\n", job.ID, job.Command)
		return job, nil
	}
	return job, err
}

func (jm *JobManager) WaitForForeground(job *Job, ctx *ExecCtx) (bool, error) {
	if job == nil {
		return false, fmt.Errorf("job not found")
	}
	out := ctx.Out
	if ctx.SystemIO != nil {
		out = ctx.SystemIO.Out
	}
	job.ioMu.Lock()
	if err := job.resumeDisplayLocked(out); err != nil {
		job.ioMu.Unlock()
		return false, err
	}
	if snapshot := job.Snapshot(); snapshot.State == JobDone {
		job.ioMu.Unlock()
		job.restoreTerminal(out)
		return false, snapshot.ExitErr
	}
	job.foreground = out
	job.ioMu.Unlock()

	jm.mu.Lock()
	jm.fgJob = job
	jm.mu.Unlock()
	defer func() {
		job.ioMu.Lock()
		job.foreground = nil
		job.ioMu.Unlock()
		jm.mu.Lock()
		if jm.fgJob == job {
			jm.fgJob = nil
		}
		jm.mu.Unlock()
		job.restoreTerminal(out)
	}()

	in := ctx.In
	if ctx.SystemIO != nil {
		in = ctx.SystemIO.In
	}
	input, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		<-job.Done()
		return false, job.Snapshot().ExitErr
	}

	pollfds := []unix.PollFd{{Fd: int32(input.Fd()), Events: unix.POLLIN}}
	buffer := make([]byte, 4096)
	for {
		select {
		case <-job.Done():
			return false, job.Snapshot().ExitErr
		default:
		}
		n, err := unix.Poll(pollfds, 50)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return false, err
		}
		if n == 0 || pollfds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, err = input.Read(buffer)
		if err != nil {
			return false, err
		}
		if idx := bytes.IndexByte(buffer[:n], 0x1a); idx >= 0 {
			if idx > 0 && job.ptyMaster != nil {
				_, _ = job.ptyMaster.Write(buffer[:idx])
			}
			// Whatever was typed after the Ctrl+Z was meant for the shell, not
			// the job, so hand it back to the line editor instead of dropping it.
			if rest := buffer[idx+1 : n]; len(rest) > 0 && ctx.Pushback != nil {
				ctx.Pushback(rest)
			}
			return true, nil
		}
		if job.ptyMaster == nil {
			// A builtin running as a job has no PTY to forward to.  Keep reading
			// so Ctrl+Z still works rather than wedging the shell.
			continue
		}
		if _, err := job.ptyMaster.Write(buffer[:n]); err != nil {
			select {
			case <-job.Done():
				return false, job.Snapshot().ExitErr
			default:
				return false, err
			}
		}
	}
}

// resumeDisplayLocked catches the terminal up when a job returns to the
// foreground.  A full-screen job gets its modes re-established and is asked to
// repaint: replaying its buffered frames would flush a burst of stale screens,
// and once the ring buffer has wrapped the replay can start partway through an
// escape sequence.  Everything else replays the output it produced while it was
// away, which is what you want from a build or a test run.
//
// Callers must hold job.ioMu.
func (job *Job) resumeDisplayLocked(out io.Writer) error {
	if job.term.AltScreen() {
		if seq := job.term.EnterSequence(); len(seq) > 0 {
			if _, err := out.Write(seq); err != nil {
				return err
			}
		}
		job.displayedBytes = job.totalBytes
		job.requestRepaint()
		return nil
	}

	data := job.output.Bytes()
	retainedFrom := job.totalBytes - uint64(len(data))
	offset := uint64(len(data))
	if job.displayedBytes <= retainedFrom {
		offset = 0
	} else if job.displayedBytes < job.totalBytes {
		offset = job.displayedBytes - retainedFrom
	}
	if _, err := out.Write(data[offset:]); err != nil {
		return err
	}
	job.displayedBytes = job.totalBytes
	return nil
}

// restoreTerminal undoes whatever the job did to the terminal.  This runs when
// the job is backgrounded and when it exits — including when it exits badly,
// which is the case that otherwise leaves the shell drawing its prompt onto an
// alternate screen with the cursor hidden.
func (job *Job) restoreTerminal(out io.Writer) {
	if seq := job.term.LeaveSequence(); len(seq) > 0 {
		_, _ = out.Write(seq)
	}
}

// requestRepaint nudges a full-screen job into redrawing itself.  Re-sending
// the same window size would not do it — the kernel only raises SIGWINCH when
// the size actually changes — so deliver the signal directly.
func (job *Job) requestRepaint() {
	_ = job.Signal(syscall.SIGWINCH)
}

func (jm *JobManager) startPTYCommand(job *Job, command *Command) error {
	cmd := exec.Command(command.Name(), tokenValues(command.Args())...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	job.ptyMaster = ptmx
	// pty.Start puts the child in its own session, so it leads its own process
	// group and can be signalled as a unit along with anything it spawns.
	job.setPgid(cmd.Process.Pid)
	clearActive := SetCurrentCmd(cmd)
	clearTracked := job.trackCmd(cmd)
	outputDone := job.capture(ptmx)
	go func() {
		err := cmd.Wait()
		clearActive()
		clearTracked()
		<-outputDone
		job.finish(err)
	}()
	return nil
}

// startPTYPipeline runs an entire pipeline on one PTY, so a full-screen program
// anywhere in it behaves the same as it would on its own.  Without this a
// backgrounded pipeline has no PTY at all, which leaves `fg` with nothing to
// forward input to.
func (jm *JobManager) startPTYPipeline(job *Job, pipeline *Pipeline, ctx *ExecCtx) error {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return err
	}
	job.ptyMaster = ptmx

	group := &procGroup{onLeader: job.setPgid}
	jobCtx := &ExecCtx{
		In:          tty,
		Out:         tty,
		ErrOut:      tty,
		JobMgr:      ctx.JobMgr,
		TrackCmd:    job.trackCmd,
		PrepareProc: group.prepare,
	}
	outputDone := job.capture(ptmx)
	go func() {
		err := pipeline.Exec(jobCtx)
		// Dropping the shell's handle on the slave lets the master see EOF once
		// every stage has exited.
		_ = tty.Close()
		<-outputDone
		job.finish(err)
	}()
	return nil
}

func (job *Job) setPgid(pgid int) {
	job.mu.Lock()
	if pgid > 0 {
		job.pgid = pgid
	}
	job.mu.Unlock()
}

func (jm *JobManager) startBufferedExecutable(job *Job, exe Executable, ctx *ExecCtx) error {
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	jobCtx := *ctx
	jobCtx.In = strings.NewReader("")
	jobCtx.Out = writer
	jobCtx.ErrOut = writer
	jobCtx.SystemIO = nil
	jobCtx.RestoreTerm = nil
	jobCtx.TrackCmd = job.trackCmd
	outputDone := job.capture(reader)
	go func() {
		err := exe.Exec(&jobCtx)
		_ = writer.Close()
		<-outputDone
		job.finish(err)
	}()
	return nil
}

func (job *Job) capture(reader *os.File) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				job.ioMu.Lock()
				job.term.Feed(buffer[:n])
				_, _ = job.output.Write(buffer[:n])
				job.totalBytes += uint64(n)
				if job.foreground != nil {
					_, _ = job.foreground.Write(buffer[:n])
					job.displayedBytes = job.totalBytes
				}
				job.ioMu.Unlock()
			}
			if err != nil {
				break
			}
		}
		_ = reader.Close()
		close(done)
	}()
	return done
}

func (job *Job) finish(err error) {
	job.mu.Lock()
	job.State = JobDone
	job.ExitErr = err
	job.ExitCode = exitCode(err)
	job.mu.Unlock()
	close(job.done)
}

func (job *Job) trackCmd(cmd *exec.Cmd) func() {
	job.mu.Lock()
	if job.cmd == nil {
		job.cmd = cmd
	}
	job.cmds[cmd] = struct{}{}
	job.mu.Unlock()
	return func() {
		job.mu.Lock()
		delete(job.cmds, cmd)
		job.mu.Unlock()
	}
}

func (job *Job) Snapshot() Job {
	job.mu.Lock()
	defer job.mu.Unlock()
	return Job{
		ID:        job.ID,
		Command:   job.Command,
		Dir:       job.Dir,
		StartTime: job.StartTime,
		State:     job.State,
		ExitCode:  job.ExitCode,
		ExitErr:   job.ExitErr,
	}
}

func (job *Job) Output() []byte {
	return job.output.Bytes()
}

func (job *Job) Done() <-chan struct{} {
	return job.done
}

// Signal delivers sig to every process making up the job.
func (job *Job) Signal(sig os.Signal) error {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.State == JobDone {
		return fmt.Errorf("job %d is already done", job.ID)
	}
	// Signal the group first — that is what reaches anything the job spawned.
	// Then signal the tracked children directly: a pipeline's later stages run
	// outside the leader's group (see procGroup), so the group signal alone
	// would leave them running.  Delivering twice to the leader is harmless.
	delivered := false
	signum, isUnix := sig.(syscall.Signal)
	if isUnix && job.pgid > 0 {
		if err := syscall.Kill(-job.pgid, signum); err == nil {
			delivered = true
		}
	}
	var firstErr error
	for cmd := range job.cmds {
		if cmd.Process == nil {
			continue
		}
		if err := cmd.Process.Signal(sig); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delivered = true
	}
	if delivered {
		return nil
	}
	return firstErr
}

func (jm *JobManager) ListJobs() []*Job {
	jm.mu.Lock()
	jobs := make([]*Job, 0, len(jm.jobs))
	for _, job := range jm.jobs {
		jobs = append(jobs, job)
	}
	jm.mu.Unlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	return jobs
}

func (jm *JobManager) GetJob(id int) *Job {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	return jm.jobs[id]
}

func (jm *JobManager) Reap() []*Job {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	var completed []*Job
	for _, job := range jm.jobs {
		job.mu.Lock()
		if job.State == JobDone && !job.reported {
			job.reported = true
			completed = append(completed, job)
		}
		job.mu.Unlock()
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].ID < completed[j].ID })
	return completed
}

func (job *Job) resize(width, height int) {
	if job.ptyMaster == nil || width <= 0 || height <= 0 {
		return
	}
	_ = pty.Setsize(job.ptyMaster, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
}

func (jm *JobManager) ResizeForeground(width, height int) {
	jm.mu.Lock()
	job := jm.fgJob
	jm.mu.Unlock()
	if job != nil {
		job.resize(width, height)
	}
}

// WarnBeforeExit reports the number of running jobs and whether the shell
// should warn instead of exiting.  The warning re-arms as soon as any other
// command runs (see ClearExitWarning), so a warning from earlier in the session
// cannot silently authorise killing jobs started since.
func (jm *JobManager) WarnBeforeExit() (int, bool) {
	running := jm.RunningCount()
	if running == 0 {
		return 0, false
	}
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if jm.exitWarned {
		return running, false
	}
	jm.exitWarned = true
	return running, true
}

func (jm *JobManager) ClearExitWarning() {
	jm.mu.Lock()
	jm.exitWarned = false
	jm.mu.Unlock()
}

func (jm *JobManager) RunningCount() int {
	count := 0
	for _, job := range jm.ListJobs() {
		if job.Snapshot().State == JobRunning {
			count++
		}
	}
	return count
}

func (jm *JobManager) Shutdown() {
	jobs := jm.ListJobs()
	for _, job := range jobs {
		if job.Snapshot().State == JobRunning {
			_ = job.Signal(syscall.SIGKILL)
		}
	}
	for _, job := range jobs {
		if job.Snapshot().State != JobRunning {
			continue
		}
		select {
		case <-job.Done():
		case <-time.After(time.Second):
			if job.ptyMaster != nil {
				_ = job.ptyMaster.Close()
			}
		}
	}
}
