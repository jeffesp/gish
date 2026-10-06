package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// enableTraces turns tracing back on (TestMain disables it) and points it at a
// not-yet-existing nested directory so directory creation is exercised too.
func enableTraces(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "a", "b")
	t.Setenv("GISH_TRACES", "1")
	t.Setenv("GISH_TRACE_DIR", dir)
	return dir
}

func traceFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read trace dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

func jsCmd(line string) *Command {
	return &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "js"}, {Kind: TokenWord, Value: strings.TrimPrefix(line, "js ")}},
		Line:   line,
	}
}

func TestTraceDirPrecedence(t *testing.T) {
	t.Setenv("GISH_TRACE_DIR", "/custom/traces")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	if got := traceDir(); got != "/custom/traces" {
		t.Errorf("GISH_TRACE_DIR: got %q", got)
	}

	t.Setenv("GISH_TRACE_DIR", "")
	if got, want := traceDir(), filepath.Join("/xdg/state", "gish", "traces"); got != want {
		t.Errorf("XDG_STATE_HOME: got %q want %q", got, want)
	}

	t.Setenv("XDG_STATE_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got, want := traceDir(), filepath.Join(home, ".local", "state", "gish", "traces"); got != want {
		t.Errorf("default: got %q want %q", got, want)
	}
}

func TestTraceJSErrorWritesFile(t *testing.T) {
	dir := enableTraces(t)
	buf, ctx := initTestVM(t)
	buf.Reset()

	err := builtinJS(jsCmd(`js function boomer() { throw new Error("boom") } boomer()`), ctx)
	if err == nil {
		t.Fatal("expected error")
	}

	files := traceFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want 1 trace file, got %d", len(files))
	}
	if !strings.Contains(err.Error(), "(trace: "+files[0]+")") {
		t.Errorf("error %q does not name trace %q", err, files[0])
	}
	if !strings.HasPrefix(err.Error(), "Error: boom (trace: ") {
		t.Errorf("unexpected error message %q", err)
	}
	data, rerr := os.ReadFile(files[0])
	if rerr != nil {
		t.Fatal(rerr)
	}
	body := string(data)
	for _, want := range []string{"origin:  js", "Error: boom", "at boomer"} {
		if !strings.Contains(body, want) {
			t.Errorf("trace missing %q:\n%s", want, body)
		}
	}
}

func TestTraceDisabled(t *testing.T) {
	dir := enableTraces(t)
	t.Setenv("GISH_TRACES", "0")
	_, ctx := initTestVM(t)

	err := builtinJS(jsCmd(`js throw new Error("boom")`), ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "Error: boom" {
		t.Errorf("got %q want %q", got, "Error: boom")
	}
	if _, serr := os.Stat(dir); !os.IsNotExist(serr) {
		t.Errorf("trace dir should not be created when disabled (stat err %v)", serr)
	}
}

func TestTraceRegisterCallback(t *testing.T) {
	dir := enableTraces(t)
	buf, ctx := initTestVM(t)
	defer delete(Builtins, "failer")

	if _, err := jsVM.RunString(`gish.register("failer", function(ctx) { throw new Error("bad cb") })`); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	err := Builtins["failer"](&Command{Tokens: []Token{{Kind: TokenWord, Value: "failer"}}, Line: "failer"}, ctx)
	if err == nil || !strings.Contains(err.Error(), "bad cb") || !strings.Contains(err.Error(), "(trace: ") {
		t.Fatalf("unexpected error %v", err)
	}
	files := traceFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want 1 trace file, got %d", len(files))
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), "origin:  register:failer") {
		t.Errorf("trace missing origin:\n%s", data)
	}
}

func TestTracePromptDeduped(t *testing.T) {
	dir := enableTraces(t)
	buf, _ := initTestVM(t)
	defer func() { promptFn = nil }()

	if _, err := jsVM.RunString(`gish.setPrompt(function() { throw new Error("bad prompt") })`); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	for i := 0; i < 3; i++ {
		if got := JSPrompt(); got != defaultPrompt {
			t.Errorf("prompt %q, want default", got)
		}
	}
	files := traceFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want 1 trace file for repeated prompt error, got %d", len(files))
	}
	if out := buf.String(); strings.Count(out, "gish: prompt error:") != 1 || !strings.Contains(out, files[0]) {
		t.Errorf("unexpected prompt error output %q", out)
	}
}

func TestTraceInitScript(t *testing.T) {
	dir := enableTraces(t)
	cfg := t.TempDir()
	t.Setenv("GISH_CONFIG_DIR", cfg)
	if err := os.WriteFile(filepath.Join(cfg, "init.js"), []byte(`function setup() { null.x } setup()`), 0644); err != nil {
		t.Fatal(err)
	}
	buf, _ := initTestVM(t)

	files := traceFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want 1 trace file, got %d", len(files))
	}
	if !strings.Contains(buf.String(), "gish: init.js: ") || !strings.Contains(buf.String(), "(trace: "+files[0]+")") {
		t.Errorf("unexpected stderr %q", buf.String())
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), "at setup") {
		t.Errorf("trace missing stack:\n%s", data)
	}
}

func TestCleanupTraces(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	fresh := filepath.Join(dir, "fresh.txt")
	other := filepath.Join(dir, "old.keep")
	for _, p := range []string{old, fresh, other} {
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-10 * 24 * time.Hour)
	for _, p := range []string{old, other} {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}

	cleanupTraces(dir, 7*24*time.Hour)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old trace should be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh trace should remain: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("non-.txt file should remain: %v", err)
	}

	missing := filepath.Join(dir, "missing")
	cleanupTraces(missing, time.Hour)
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("cleanup must not create the dir")
	}
}

func TestKeepTraceDays(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want int
	}{{"", 7}, {"3", 3}, {"0", 0}, {"-1", -1}, {"junk", 7}} {
		t.Setenv("GISH_KEEP_TRACE_DAYS", tc.val)
		if got := keepTraceDays(); got != tc.want {
			t.Errorf("GISH_KEEP_TRACE_DAYS=%q: got %d want %d", tc.val, got, tc.want)
		}
	}
}
