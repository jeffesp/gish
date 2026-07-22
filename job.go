package main

import (
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
	}
	if command, ok := exe.(*Command); ok {
		if _, builtin := Builtins[command.Name()]; !builtin {
			if err := jm.startPTYCommand(job, command); err != nil {
				return nil, err
			}
			return job, nil
		}
	}
	if err := jm.startBufferedExecutable(job, exe, ctx); err != nil {
		return nil, err
	}
	return job, nil
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
	data := job.output.Bytes()
	retainedFrom := job.totalBytes - uint64(len(data))
	offset := uint64(len(data))
	if job.displayedBytes <= retainedFrom {
		offset = 0
	} else if job.displayedBytes < job.totalBytes {
		offset = job.displayedBytes - retainedFrom
	}
	if _, err := out.Write(data[offset:]); err != nil {
		job.ioMu.Unlock()
		return false, err
	}
	job.displayedBytes = job.totalBytes
	if snapshot := job.Snapshot(); snapshot.State == JobDone {
		job.ioMu.Unlock()
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
	}()

	in := ctx.In
	if ctx.SystemIO != nil {
		in = ctx.SystemIO.In
	}
	input, ok := in.(*os.File)
	if !ok || job.ptyMaster == nil || !term.IsTerminal(int(input.Fd())) {
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
		for i, b := range buffer[:n] {
			if b == 0x1a {
				if i > 0 {
					_, _ = job.ptyMaster.Write(buffer[:i])
				}
				return true, nil
			}
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

func (jm *JobManager) startPTYCommand(job *Job, command *Command) error {
	cmd := exec.Command(command.Name(), tokenValues(command.Args())...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	job.ptyMaster = ptmx
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

func (job *Job) Signal(sig os.Signal) error {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.State == JobDone {
		return fmt.Errorf("job %d is already done", job.ID)
	}
	var firstErr error
	for cmd := range job.cmds {
		if cmd.Process != nil {
			if err := cmd.Process.Signal(sig); err != nil && firstErr == nil {
				firstErr = err
			}
		}
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
