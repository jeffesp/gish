package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func init() {
	RegisterBuiltin("jobs", builtinJobs)
	RegisterBuiltin("joblog", builtinJoblog)
	RegisterBuiltin("fg", builtinForegroundJob)
	RegisterBuiltin("kill", builtinKillJob)
}

func builtinJobs(cmd *Command, ctx *ExecCtx) error {
	if len(cmd.Args()) != 0 {
		return fmt.Errorf("usage: jobs")
	}
	if ctx.JobMgr == nil {
		return fmt.Errorf("job control is unavailable")
	}
	fmt.Fprintln(ctx.Out, "  ID  STATE     STARTED       DIR                 COMMAND")
	for _, job := range ctx.JobMgr.ListJobs() {
		snapshot := job.Snapshot()
		state := "running"
		if snapshot.State == JobDone {
			state = fmt.Sprintf("done(%d)", snapshot.ExitCode)
		}
		fmt.Fprintf(ctx.Out, "  %-3d %-9s %-13s %-19s %s\n", snapshot.ID, state, formatJobAge(time.Since(snapshot.StartTime)), displayJobDir(snapshot.Dir), snapshot.Command)
	}
	return nil
}

func builtinJoblog(cmd *Command, ctx *ExecCtx) error {
	if len(cmd.Args()) != 1 {
		return fmt.Errorf("usage: joblog N")
	}
	job, err := jobFromArg(ctx, cmd.Args()[0].Value)
	if err != nil {
		return err
	}
	if ctx.SystemIO == nil {
		_, err = ctx.Out.Write(job.Output())
		return err
	}
	pager := strings.Fields(os.Getenv("PAGER"))
	if len(pager) == 0 {
		pager = []string{"less", "-R"}
	}
	pagerCmd := exec.Command(pager[0], pager[1:]...)
	pagerCmd.Stdin = bytes.NewReader(job.Output())
	pagerCmd.Stdout = ctx.SystemIO.Out
	pagerCmd.Stderr = ctx.SystemIO.ErrOut
	if ctx.RestoreTerm != nil {
		reenter := ctx.RestoreTerm()
		defer reenter()
	}
	return pagerCmd.Run()
}

func builtinForegroundJob(cmd *Command, ctx *ExecCtx) error {
	if ctx.JobMgr == nil {
		return fmt.Errorf("job control is unavailable")
	}
	args := cmd.Args()
	var job *Job
	switch len(args) {
	case 0:
		jobs := ctx.JobMgr.ListJobs()
		for i := len(jobs) - 1; i >= 0; i-- {
			if jobs[i].Snapshot().State == JobRunning {
				job = jobs[i]
				break
			}
		}
		if job == nil {
			return fmt.Errorf("no running jobs")
		}
	case 1:
		var err error
		job, err = jobFromArg(ctx, args[0].Value)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("usage: fg [N]")
	}
	backgrounded, err := ctx.JobMgr.WaitForForeground(job, ctx)
	if backgrounded {
		fmt.Fprintf(ctx.Out, "[%d] backgrounded: %s\n", job.ID, job.Command)
	}
	return err
}

func builtinKillJob(cmd *Command, ctx *ExecCtx) error {
	args := cmd.Args()
	sig := os.Signal(syscall.SIGTERM)
	if len(args) == 2 && args[0].Value == "-9" {
		sig = syscall.SIGKILL
		args = args[1:]
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: kill [-9] N")
	}
	job, err := jobFromArg(ctx, args[0].Value)
	if err != nil {
		return err
	}
	return job.Signal(sig)
}

func jobFromArg(ctx *ExecCtx, value string) (*Job, error) {
	if ctx.JobMgr == nil {
		return nil, fmt.Errorf("job control is unavailable")
	}
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("invalid job ID %q", value)
	}
	job := ctx.JobMgr.GetJob(id)
	if job == nil {
		return nil, fmt.Errorf("job %d not found", id)
	}
	return job, nil
}

func formatJobAge(d time.Duration) string {
	if d < time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds ago", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm ago", int(d.Hours()), int(d.Minutes())%60)
}

func displayJobDir(dir string) string {
	home, err := os.UserHomeDir()
	if err == nil && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}
