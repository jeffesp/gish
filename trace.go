package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
)

// JS error traces: each JS error is written to its own file under traceDir()
// with the full goja stack, and the path is appended to the user-facing error.
//
//	GISH_TRACES=0|off|false   disable writing and cleanup
//	GISH_TRACE_DIR            override the trace directory
//	GISH_KEEP_TRACE_DAYS      delete traces older than this at startup (default 7, <=0 keeps forever)

const defaultKeepTraceDays = 7

var (
	traceSeq         atomic.Uint64
	traceCleanupOnce sync.Once
)

func tracesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GISH_TRACES"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

func traceDir() string {
	if d := os.Getenv("GISH_TRACE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "gish", "traces")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gish", "traces")
}

// jsErrorText splits a JS error into the short message shown to the user and
// the detailed text (message plus stack frames) written to the trace file.
// It may call into the JS runtime, so call it while holding jsmu.
func jsErrorText(err error) (msg, detail string) {
	if err == nil {
		return "", ""
	}
	var ex *goja.Exception
	if !errors.As(err, &ex) {
		return err.Error(), err.Error()
	}
	msg = ex.Value().String()
	if outer, ok := err.(*goja.Exception); !ok || outer != ex {
		// err wraps the exception (e.g. "path: <exception>"); keep the prefix.
		msg = err.Error()
	}

	// Walk nested exceptions (a JS throw that wraps a Go error that wraps
	// another JS exception, as with gish.source) so inner stacks are kept.
	var b strings.Builder
	var cur error = err
	for cur != nil {
		if errors.As(cur, &ex) {
			if b.Len() > 0 {
				b.WriteString("caused by: ")
			}
			b.WriteString(ex.String())
			cur = ex.Unwrap()
			continue
		}
		break
	}
	return msg, b.String()
}

// writeJSTrace writes detail to a new trace file and returns its path, or ""
// if tracing is disabled or the write failed.
func writeJSTrace(origin, detail string) string {
	if !tracesEnabled() || detail == "" {
		return ""
	}
	dir := traceDir()
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ""
	}

	now := time.Now()
	name := fmt.Sprintf("%s-%d-%d.txt", now.Format("20060102-150405.000"), os.Getpid(), traceSeq.Add(1))
	path := filepath.Join(dir, name)

	cwd, _ := os.Getwd()
	var b strings.Builder
	fmt.Fprintf(&b, "time:    %s\n", now.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, "version: %s\n", Version)
	fmt.Fprintf(&b, "pid:     %d\n", os.Getpid())
	fmt.Fprintf(&b, "cwd:     %s\n", cwd)
	fmt.Fprintf(&b, "origin:  %s\n\n", origin)
	b.WriteString(detail)
	if !strings.HasSuffix(detail, "\n") {
		b.WriteByte('\n')
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ""
	}
	_, werr := f.WriteString(b.String())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return ""
	}
	return path
}

// withTracePath appends the trace file location to msg when one was written.
func withTracePath(msg, path string) string {
	if path == "" {
		return msg
	}
	return fmt.Sprintf("%s (trace: %s)", msg, path)
}

// traceJSError writes a trace for a JS error and returns the user-facing
// error. msg and detail come from jsErrorText; call this without holding jsmu.
func traceJSError(origin, msg, detail string) error {
	return errors.New(withTracePath(msg, writeJSTrace(origin, detail)))
}

func keepTraceDays() int {
	if v := os.Getenv("GISH_KEEP_TRACE_DAYS"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return defaultKeepTraceDays
}

// startTraceCleanup removes stale trace files in the background, once per process.
func startTraceCleanup() {
	traceCleanupOnce.Do(func() {
		if !tracesEnabled() {
			return
		}
		days := keepTraceDays()
		if days <= 0 {
			return
		}
		dir := traceDir()
		if dir == "" {
			return
		}
		go cleanupTraces(dir, time.Duration(days)*24*time.Hour)
	})
}

// cleanupTraces deletes *.txt files in dir older than maxAge. A missing dir is
// not an error and is never created here.
func cleanupTraces(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".txt" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
