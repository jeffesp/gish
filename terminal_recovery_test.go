package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// The programs under testdata/badcmds each wreck the terminal in a specific way
// and exit without cleaning up.  The shell has to put things back regardless,
// so drive the real binary under a real PTY and check what it wrote.

type ptySession struct {
	t    *testing.T
	cmd  *exec.Cmd
	ptmx *os.File
	mu   sync.Mutex
	out  bytes.Buffer
}

func startGish(t *testing.T, gish string) *ptySession {
	t.Helper()
	cmd := exec.Command(gish)
	cmd.Env = append(os.Environ(), "GISH_HISTORY_FILE="+filepath.Join(t.TempDir(), "history"))
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("start gish under pty: %v", err)
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		t.Fatalf("set pty size: %v", err)
	}
	session := &ptySession{t: t, cmd: cmd, ptmx: ptmx}
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buffer)
			if n > 0 {
				session.mu.Lock()
				session.out.Write(buffer[:n])
				session.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		_, _ = cmd.Process.Wait()
	})
	return session
}

func (s *ptySession) send(text string) {
	s.t.Helper()
	if _, err := io.WriteString(s.ptmx, text); err != nil {
		s.t.Fatalf("write to pty: %v", err)
	}
}

// waitFor polls until the session output contains want, so tests do not depend
// on fixed sleeps.  It returns everything captured so far.
func (s *ptySession) waitFor(want string, timeout time.Duration) string {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		got := s.out.String()
		s.mu.Unlock()
		if strings.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %q in output:\n%q", want, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *ptySession) captured() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

func (s *ptySession) promptCount() int {
	return strings.Count(s.captured(), "gish>")
}

// run sends a command and waits for the shell to come back to a prompt.
// Synchronising on the prompt rather than on the command's own output matters:
// the line discipline echoes what we type, so waiting for a marker string can
// match the echo of the command that was meant to produce it and return before
// the command has even run.
func (s *ptySession) run(line string, timeout time.Duration) string {
	s.t.Helper()
	want := s.promptCount() + 1
	s.send(line + "\r")
	deadline := time.Now().Add(timeout)
	for {
		if s.promptCount() >= want {
			return s.captured()
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for prompt after %q:\n%q", line, s.captured())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func buildBinary(t *testing.T, pkg, name string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", out, pkg)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, output)
	}
	return out
}

func TestTerminalRecoveredAfterBadCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and spawns PTYs")
	}
	gish := buildBinary(t, ".", "gish")

	cases := []struct {
		name    string
		restore []string
	}{
		{"alt_screen_crash", []string{"\x1b[?1049l"}},
		{"cursor_hide", []string{"\x1b[?25h"}},
		{"mouse_mode", []string{"\x1b[?1000l", "\x1b[?1006l"}},
		{"bracketed_paste", []string{"\x1b[?2004l"}},
		{"scroll_region", []string{"\x1b[r"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			bad := buildBinary(t, "./testdata/badcmds/"+testCase.name, testCase.name)
			session := startGish(t, gish)
			session.waitFor("gish>", 10*time.Second)
			got := session.run(bad, 10*time.Second)

			for _, want := range testCase.restore {
				if !strings.Contains(got, want) {
					t.Errorf("terminal not restored: output missing %q\n%q", want, got)
				}
			}
		})
	}
}

func TestBackgroundingRestoresTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and spawns PTYs")
	}
	gish := buildBinary(t, ".", "gish")
	// alt_screen_crash exits immediately; use a job that stays up so Ctrl+Z has
	// something to background.
	fullscreen := buildBinary(t, "./testdata/badcmds/alt_screen_hold", "alt_screen_hold")

	session := startGish(t, gish)
	session.waitFor("gish>", 10*time.Second)
	session.send(fullscreen + "\r")
	session.waitFor("HOLDING", 10*time.Second)

	session.send("\x1a") // Ctrl+Z
	got := session.waitFor("backgrounded", 10*time.Second)

	restore := strings.Index(got, "\x1b[?1049l")
	if restore < 0 {
		t.Fatalf("backgrounding left the terminal on the alternate screen:\n%q", got)
	}
	if !strings.Contains(got, "\x1b[?25h") {
		t.Errorf("backgrounding left the cursor hidden:\n%q", got)
	}
	if restore > strings.Index(got, "backgrounded") {
		t.Error("terminal should be restored before the prompt returns")
	}

	// The job must still be alive, and listed.
	session.send("jobs\r")
	jobs := session.waitFor("STATE", 10*time.Second)
	listing := jobs[strings.Index(jobs, "STATE"):]
	if !strings.Contains(listing, "running") {
		t.Errorf("backgrounded job should still be running:\n%q", listing)
	}
}

func TestForegroundingFullScreenJobSkipsReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and spawns PTYs")
	}
	gish := buildBinary(t, ".", "gish")
	fullscreen := buildBinary(t, "./testdata/badcmds/alt_screen_hold", "alt_screen_hold")

	session := startGish(t, gish)
	session.waitFor("gish>", 10*time.Second)
	session.send(fullscreen + "\r")
	session.waitFor("HOLDING", 10*time.Second)
	session.send("\x1a")
	session.waitFor("backgrounded", 10*time.Second)

	// Let the job pile up frames while it is in the background.
	time.Sleep(1500 * time.Millisecond)
	before := strings.Count(session.captured(), "FRAME")

	session.send("fg 1\r")
	session.waitFor("\x1b[?1049h", 10*time.Second)
	time.Sleep(300 * time.Millisecond)
	after := strings.Count(session.captured(), "FRAME")

	// A replay would dump every frame buffered while backgrounded at once.
	if after-before > 6 {
		t.Errorf("fg replayed %d buffered frames; expected a repaint instead", after-before)
	}
}

func TestForegroundOnJobWithoutPTYDoesNotHang(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and spawns PTYs")
	}
	gish := buildBinary(t, ".", "gish")
	session := startGish(t, gish)
	session.waitFor("gish>", 10*time.Second)

	// A pipeline gets a PTY now, so foregrounding it must stay interruptible.
	session.send("sleep 30 | cat &\r")
	session.waitFor("started", 10*time.Second)
	session.send("fg 1\r")
	time.Sleep(500 * time.Millisecond)
	session.send("\x1a")
	session.waitFor("backgrounded", 10*time.Second)

	// The shell has to be responsive again.
	session.run("echo alive", 10*time.Second)
}

func TestChildrenCanStillBeSuspended(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and spawns PTYs")
	}
	gish := buildBinary(t, ".", "gish")
	probe := buildBinary(t, "./testdata/badcmds/tstp_probe", "tstp_probe")

	session := startGish(t, gish)
	session.waitFor("gish>", 10*time.Second)
	got := session.run(probe, 10*time.Second)

	// The shell must not leave children with SIG_IGN for SIGTSTP: an inherited
	// ignored disposition cannot be undone by the child, so job control inside
	// anything gish launches would silently stop working.
	if strings.Contains(got, "SURVIVED-SIGTSTP") {
		t.Errorf("child inherited SIG_IGN for SIGTSTP; job control inside children is broken:\n%q", got)
	}
}
