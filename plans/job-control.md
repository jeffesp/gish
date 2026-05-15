# Job Control for Gish

## Context

Gish currently runs every command synchronously — the REPL blocks until it finishes. There's no way to run multiple things at once without opening separate terminal windows. The goal is to let the user manage multiple concurrent jobs within a single gish session, with interactions that are easy and intuitive — not the clunky POSIX `fg`/`bg`/`jobs` workflow.

Key design decisions (confirmed with user):
- **Backgrounded jobs keep running** (not POSIX-suspended). Matches the mental model of switching terminal windows.
- **PTY per job** via `creack/pty`. Programs like vim/htop work correctly when backgrounded and resumed.
- **Keybinding-driven job picker** (Ctrl+J) opens an interactive list, plus text commands (`jobs`, `fg N`) as fallback.
- **Incremental delivery** in 3 phases.

## Phase 1: Background Jobs Foundation

Start jobs in background with `&`, list them, read their output.

### 1. Add `RingBuffer` type

New file: `ringbuffer.go`

- Capped circular byte buffer (default 1MB, configurable via `GISH_JOB_BUFFER_SIZE`)
- Implements `io.Writer`
- `Bytes() []byte` returns contents in order
- Uses `sync.RWMutex` (concurrent writes from PTY reader, reads from `joblog`)

### 2. Add `Job` and `JobManager`

New file: `job.go`

```go
type JobState int
const (
    JobRunning JobState = iota
    JobDone
)

type Job struct {
    ID        int
    Command   string          // command line as typed
    Dir       string          // working directory at start
    StartTime time.Time
    State     JobState
    ExitCode  int
    ExitErr   error
    ptyMaster *os.File        // master side of PTY
    cmd       *exec.Cmd       // underlying process
    output    *RingBuffer     // captured stdout+stderr
    done      chan struct{}    // closed when process exits
    mu        sync.Mutex
}

type JobManager struct {
    mu     sync.Mutex
    jobs   map[int]*Job
    nextID int
    fgJob  *Job              // currently foregrounded job (nil = at prompt)
}
```

`JobManager` methods:
- `StartBackground(exe Executable, line string, ctx *ExecCtx) (*Job, error)` — allocates PTY via `pty.Start(cmd)`, starts output capture goroutine (reads PTY master → writes to RingBuffer), returns immediately
- `ListJobs() []*Job` — returns snapshot of all jobs
- `GetJob(id int) *Job`
- `Reap()` — called at prompt display, returns list of newly-completed jobs for notification
- `Shutdown()` — kills all running jobs, closes PTYs (called on shell exit)

Output capture goroutine per job:
```
go func() {
    io.Copy(job.output, job.ptyMaster)  // blocks until PTY EOF
    job.mu.Lock()
    job.State = JobDone
    // collect exit code from cmd.Wait()
    job.mu.Unlock()
    close(job.done)
}()
```

### 3. Add `TokenBackground` to tokenizer

File: `token.go`

- Add `TokenBackground` token kind
- In `tokenize()`: recognize bare `&` at end of token stream (not `|&` which is already `TokenMergePipe`)
- In `parseTokens()`: strip trailing `TokenBackground`, return the `Executable` plus a `background bool` flag. Change return signature to `(Executable, bool, error)` or wrap in a struct.

### 4. Wire `execLine` to support background execution

File: `repl.go`

- `parseTokens` now returns a background flag
- If background: call `jobMgr.StartBackground(exe, line, ctx)`, print `[N] started: <command>`
- If foreground: run as today (`exe.Exec(ctx)`)
- After every command, call `jobMgr.Reap()` and print notifications for completed jobs: `[N] done (exit 0): <command>`

### 5. Add `jobs` and `joblog` builtins

New file: `job_builtins.go`

**`jobs` builtin:**
```
gish> jobs
  ID  STATE    STARTED      DIR              COMMAND
  1   running  2m30s ago    ~/Code/gish      make test
  3   done(0)  5m ago       ~/               sleep 300
```

**`joblog N` builtin:**
- Dumps `job.output.Bytes()` to stdout
- In Phase 3 this will pipe through `$PAGER`; for now, raw dump is fine

### 6. Register builtins and initialize JobManager

Files: `builtins.go` (register), `repl.go` (create `JobManager`, attach to context)

File: `context.go` — add `JobMgr *JobManager` field to `ExecCtx`

### 7. Add dependency

File: `go.mod` — add `github.com/creack/pty/v2`

### 8. Tests

New files: `ringbuffer_test.go`, `job_test.go`

---

## Phase 2: Foreground/Background Switching

Ctrl+Z to background a running job, `fg` to resume one.

### 1. Wrap foreground commands in Jobs

Every external command (not builtins) now starts via `JobManager.StartForeground`:
- Allocate PTY via `pty.Start(cmd)`
- Start output capture goroutine (reads PTY master → writes to **both** real terminal and RingBuffer)
- Start input forwarding goroutine (reads real stdin → writes to PTY master)
- Forward `SIGWINCH` to the job's PTY via `pty.Setsize()`
- Block until: process exits OR user presses Ctrl+Z

File: `job.go` — add `StartForeground`, `WaitForForeground` methods
File: `command.go` — `Command.Exec` routes external commands through `jobMgr.StartForeground`

### 2. Ctrl+Z handling

During foreground job execution, the shell reads stdin in a goroutine and forwards to the PTY master. The real terminal is put into raw mode for this stdin reader so that Ctrl+Z (byte 0x1A) can be detected directly rather than triggering SIGTSTP. When seen:

1. Stop the stdin-reader goroutine
2. Switch the output capture goroutine to buffer-only mode (stop copying to real terminal)
3. Re-enter raw mode for gish's line editor
4. Print `[N] backgrounded: <command>`
5. Return control to REPL loop

The job's process keeps running — it never receives SIGTSTP. It continues producing output into its PTY, which the capture goroutine buffers.

File: `signals.go` — add SIGTSTP to ignored signals (so the shell itself doesn't suspend)

### 3. `fg` builtin

File: `job_builtins.go`

`fg [N]` (default: most recently backgrounded job):
1. Exit raw mode (RestoreTerm)
2. Replay unread portion of job's output buffer to real terminal
3. Resume bidirectional copy (output capture → real terminal + buffer, stdin → PTY master)
4. Block until process exits or Ctrl+Z again

### 4. `kill` builtin

File: `job_builtins.go`

`kill [N]` — sends SIGTERM to job's process
`kill -9 [N]` — sends SIGKILL

### 5. Shell exit with running jobs

File: `builtins.go` — modify `builtinExit`:
- If jobs are running, print warning: `gish: N running jobs. Use 'exit' again to force.`
- Set a flag; on second `exit`, call `jobMgr.Shutdown()` and exit

### 6. Edge cases

- **Job exits while backgrounded:** capture goroutine sees PTY EOF, marks job Done. Notification at next prompt.
- **Ctrl+C during foreground job:** stdin reader sees 0x03, writes it to PTY master. The PTY's line discipline sends SIGINT to the job. Normal behavior.
- **Pipeline as foreground job:** The entire `Pipeline.Exec` runs inside the PTY context. All stages share one PTY. The pipeline is one job.

---

## Phase 3: Polish and Job Picker

### 1. Interactive job picker (Ctrl+J)

File: `repl.go` — wire `AutoCompleteCallback` for key `'\x0a'` (Ctrl+J, ASCII 10, confirmed available per `plans/possible-readline.md`)

When Ctrl+J is pressed at the prompt:
1. Render a job list below the prompt line (using ANSI cursor movement)
2. Arrow keys to highlight a job, Enter to foreground it, Escape to cancel
3. Show: job ID, state, runtime, directory, command (truncated to fit terminal width)
4. Implementation: direct ANSI rendering in Go (no TUI library), reads keys via raw stdin

This reuses patterns from the `keyboard-shortcuts.md` plan (AutoCompleteCallback interception).

### 2. `joblog` improvement

Pipe output through `$PAGER` (default `less -R`) instead of raw dump, so the user can scroll through large output.

### 3. Pipeline background support

`cmd1 | cmd2 &` — the entire pipeline runs as one background job. The `parseTokens` function already returns the background flag; `Pipeline.Exec` runs inside the job's PTY context.

### 4. JS API extensions

File: `scripting.go`

```javascript
gish.bg("make", ["test"])     // start background job, returns job ID
gish.jobs()                    // returns array of {id, command, state, dir, startTime, exitCode}
gish.job(1)                    // get single job info
gish.jobOutput(1)              // returns captured output as string
gish.killJob(1)                // send SIGTERM to job
```

### 5. Configurable buffer size

`GISH_JOB_BUFFER_SIZE` environment variable (default "1048576" = 1MB). Parsed in `JobManager` initialization.

---

## Files Summary

### New Files
| File | Purpose |
|------|---------|
| `ringbuffer.go` | Capped circular byte buffer |
| `ringbuffer_test.go` | Tests for RingBuffer |
| `job.go` | Job struct, JobManager, PTY lifecycle, output capture |
| `job_test.go` | Tests for job system |
| `job_builtins.go` | `jobs`, `fg`, `joblog`, `kill` builtins |

### Modified Files
| File | Changes |
|------|---------|
| `token.go` | Add `TokenBackground`, recognize `&` |
| `tok_test.go` | Tests for `&` tokenization |
| `repl.go` | Initialize JobManager, background dispatch in execLine, completion notifications, Ctrl+J picker (Phase 3) |
| `context.go` | Add `JobMgr *JobManager` field |
| `command.go` | Route external commands through JobManager for foreground PTY execution (Phase 2) |
| `signals.go` | Ignore SIGTSTP for shell process (Phase 2) |
| `builtins.go` | Register job builtins, exit-with-jobs warning |
| `scripting.go` | JS API extensions (Phase 3) |
| `go.mod` | Add `github.com/creack/pty/v2` |

## Verification

### Phase 1
- `sleep 10 &` → prints `[1] started: sleep 10`, returns to prompt
- `jobs` → shows job 1 as running
- Wait 10s, press Enter → prints `[1] done (exit 0): sleep 10`
- `echo hello &` followed by `joblog 1` → shows "hello"
- `go test ./ringbuffer_test.go ./job_test.go ./tok_test.go`

### Phase 2
- Run `vim test.txt`, press Ctrl+Z → returns to prompt, prints `[1] backgrounded`
- `jobs` → shows vim as running
- `fg 1` → vim reappears with full state intact
- Run `make test`, press Ctrl+C → make is interrupted (not the shell)
- `exit` with running jobs → shows warning, second `exit` kills all and exits
- `go test ./...`

### Phase 3
- Press Ctrl+J at prompt → interactive job list appears
- Arrow to a job, press Enter → job is foregrounded
- `joblog 1` → opens in less with scrolling
- JS: `var id = gish.bg("sleep", ["5"]); gish.jobs()` → works
- `go test ./...`
