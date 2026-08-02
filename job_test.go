package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"
)

type executableFunc func(*ExecCtx) error

func (fn executableFunc) Exec(ctx *ExecCtx) error {
	return fn(ctx)
}

func waitForJob(t *testing.T, job *Job) {
	t.Helper()
	select {
	case <-job.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("job %d did not finish", job.ID)
	}
}

func TestJobManagerStartBackground(t *testing.T) {
	jm := NewJobManager()
	ctx := &ExecCtx{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard, JobMgr: jm}
	exe := executableFunc(func(ctx *ExecCtx) error {
		_, err := io.WriteString(ctx.Out, "hello\n")
		return err
	})

	job, err := jm.StartBackground(exe, "echo hello", ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, job)

	snapshot := job.Snapshot()
	if snapshot.State != JobDone || snapshot.ExitCode != 0 {
		t.Fatalf("state=%v exit=%d", snapshot.State, snapshot.ExitCode)
	}
	if got := string(job.Output()); !strings.Contains(got, "hello") {
		t.Fatalf("output=%q", got)
	}
}

func TestJobManagerAssignsIDsAndReapsOnce(t *testing.T) {
	jm := NewJobManager()
	ctx := &ExecCtx{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard, JobMgr: jm}
	exe := executableFunc(func(*ExecCtx) error { return nil })

	first, err := jm.StartBackground(exe, "first", ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := jm.StartBackground(exe, "second", ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, first)
	waitForJob(t, second)

	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("IDs = %d, %d", first.ID, second.ID)
	}
	if got := jm.Reap(); len(got) != 2 {
		t.Fatalf("first reap returned %d jobs", len(got))
	}
	if got := jm.Reap(); len(got) != 0 {
		t.Fatalf("second reap returned %d jobs", len(got))
	}
}

func TestJobManagerUsesConfiguredBufferSize(t *testing.T) {
	t.Setenv("GISH_JOB_BUFFER_SIZE", "4")
	jm := NewJobManager()
	ctx := &ExecCtx{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard, JobMgr: jm}
	job, err := jm.StartBackground(executableFunc(func(ctx *ExecCtx) error {
		_, err := io.WriteString(ctx.Out, "abcdef")
		return err
	}), "output", ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, job)
	if got := string(job.Output()); got != "cdef" {
		t.Fatalf("output=%q", got)
	}
}

func TestJobManagerRecordsFailure(t *testing.T) {
	jm := NewJobManager()
	ctx := &ExecCtx{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard, JobMgr: jm}
	want := errors.New("failed")
	job, err := jm.StartBackground(executableFunc(func(*ExecCtx) error { return want }), "fail", ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, job)

	snapshot := job.Snapshot()
	if !errors.Is(snapshot.ExitErr, want) || snapshot.ExitCode != 1 {
		t.Fatalf("error=%v exit=%d", snapshot.ExitErr, snapshot.ExitCode)
	}
}

func TestJobBuiltins(t *testing.T) {
	jm := NewJobManager()
	var out bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &out, ErrOut: io.Discard, JobMgr: jm}
	job, err := jm.StartBackground(executableFunc(func(ctx *ExecCtx) error {
		_, err := io.WriteString(ctx.Out, "captured")
		return err
	}), "produce output", ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, job)

	if err := builtinJobs(&Command{Tokens: words("jobs")}, ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "produce output") || !strings.Contains(out.String(), "done(0)") {
		t.Fatalf("jobs output=%q", out.String())
	}
	out.Reset()
	if err := builtinJoblog(&Command{Tokens: words("joblog", "1")}, ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "captured") {
		t.Fatalf("joblog output=%q", out.String())
	}
}

func TestJobManagerStartsBackgroundPipeline(t *testing.T) {
	jm := NewJobManager()
	ctx := &ExecCtx{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard, JobMgr: jm}
	pipeline := &Pipeline{Stages: []*Command{
		{Tokens: words("printf", "hello")},
		{Tokens: words("cat")},
	}}
	job, err := jm.StartBackground(pipeline, "printf hello | cat", ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, job)
	if got := string(job.Output()); got != "hello" {
		t.Fatalf("pipeline output=%q", got)
	}
}

func TestJobManagerStartForeground(t *testing.T) {
	jm := NewJobManager()
	var out bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &out, ErrOut: &out, JobMgr: jm}
	job, err := jm.StartForeground(&Command{Tokens: words("printf", "hello")}, "printf hello", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if job.Snapshot().State != JobDone {
		t.Fatal("foreground job is not done")
	}
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("foreground output=%q", out.String())
	}
	if job.ID != 0 {
		t.Fatalf("foreground job ID=%d", job.ID)
	}
	if len(jm.ListJobs()) != 0 {
		t.Fatal("foreground job was added to the job list")
	}
	if len(jm.Reap()) != 0 {
		t.Fatal("foreground job produced a completion notification")
	}
	before := out.String()
	if _, err := jm.WaitForForeground(job, ctx); err != nil {
		t.Fatal(err)
	}
	if out.String() != before {
		t.Fatal("foreground replay duplicated already displayed output")
	}
	background, err := jm.StartBackground(executableFunc(func(*ExecCtx) error { return nil }), "background", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if background.ID != 1 {
		t.Fatalf("first background job ID=%d", background.ID)
	}
	waitForJob(t, background)
}

func TestJobSignal(t *testing.T) {
	jm := NewJobManager()
	ctx := &ExecCtx{In: strings.NewReader(""), Out: io.Discard, ErrOut: io.Discard, JobMgr: jm}
	cmd := &Command{Tokens: words("sleep", "30")}
	job, err := jm.StartBackground(cmd, "sleep 30", ctx)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		job.mu.Lock()
		started := len(job.cmds) > 0
		job.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("process did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := job.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitForJob(t, job)
	if job.Snapshot().ExitCode == 0 {
		t.Fatal("signaled job exited successfully")
	}
}
