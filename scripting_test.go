package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// initTestVM creates a fresh JS runtime for testing, wired to a buffer.
func initTestVM(t *testing.T) (*bytes.Buffer, *ExecCtx) {
	t.Helper()
	var buf bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &buf, ErrOut: &buf}
	InitScripting(ctx)
	return &buf, ctx
}

func TestJSInlineEval(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	ctx := &ExecCtx{
		In: strings.NewReader(""), Out: buf, ErrOut: buf,
	}
	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "js"}, {Kind: TokenWord, Value: "2+2"}},
		Line:   "js 2+2",
	}
	if err := builtinJS(cmd, ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "4" {
		t.Errorf("got %q want %q", got, "4")
	}
}

func TestJSInlineMultiToken(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	ctx := &ExecCtx{
		In: strings.NewReader(""), Out: buf, ErrOut: buf,
	}
	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "js"}, {Kind: TokenWord, Value: "2"}, {Kind: TokenWord, Value: "+"}, {Kind: TokenWord, Value: "3"}},
		Line:   "js 2 + 3",
	}
	if err := builtinJS(cmd, ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "5" {
		t.Errorf("got %q want %q", got, "5")
	}
}

func TestJSNoArgs(t *testing.T) {
	initTestVM(t)

	ctx := &ExecCtx{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{},
	}
	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "js"}},
		Line:   "js",
	}
	if err := builtinJS(cmd, ctx); err == nil {
		t.Error("expected error for no args")
	}
}

func TestJSException(t *testing.T) {
	initTestVM(t)

	ctx := &ExecCtx{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{},
	}
	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "js"}, {Kind: TokenWord, Value: "throw"}, {Kind: TokenWord, Value: "new"}, {Kind: TokenWord, Value: `Error("boom")`}},
		Line:   `js throw new Error("boom")`,
	}
	err := builtinJS(cmd, ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q should contain 'boom'", err.Error())
	}
}

func TestGishRegister(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	// Register a command via JS
	_, err := jsVM.RunString(`gish.register("greet", function(ctx) { gish.println("hello " + ctx.args[0]); })`)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	fn, ok := Builtins["greet"]
	if !ok {
		t.Fatal("greet not registered")
	}

	buf.Reset()
	greetCtx := &ExecCtx{
		In: strings.NewReader(""), Out: buf, ErrOut: buf,
	}
	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "greet"}, {Kind: TokenWord, Value: "world"}},
		Line:   "greet world",
	}
	if err := fn(cmd, greetCtx); err != nil {
		t.Fatalf("greet error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "hello world" {
		t.Errorf("got %q want %q", got, "hello world")
	}

	// Clean up
	delete(Builtins, "greet")
}

func TestGishRegisterReturnValue(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`gish.register("ret", function(ctx) { return 42; })`)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	fn := Builtins["ret"]
	buf.Reset()
	retCtx := &ExecCtx{
		In: strings.NewReader(""), Out: buf, ErrOut: buf,
	}
	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "ret"}},
		Line:   "ret",
	}
	if err := fn(cmd, retCtx); err != nil {
		t.Fatalf("ret error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "42" {
		t.Errorf("got %q want %q", got, "42")
	}

	delete(Builtins, "ret")
}

func TestGishPrintln(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`gish.println("hello from JS")`)
	if err != nil {
		t.Fatalf("println failed: %v", err)
	}
	if got := buf.String(); got != "hello from JS\n" {
		t.Errorf("got %q want %q", got, "hello from JS\n")
	}
}

func TestGishPrint(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`gish.print("no newline")`)
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}
	if got := buf.String(); got != "no newline" {
		t.Errorf("got %q want %q", got, "no newline")
	}
}

func TestGishPrintObject(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`gish.println({name: "gish", version: 1})`)
	if err != nil {
		t.Fatalf("println object failed: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if !strings.Contains(got, `"name"`) || !strings.Contains(got, `"gish"`) {
		t.Errorf("expected JSON object output, got %q", got)
	}
	if !strings.Contains(got, "  ") {
		t.Errorf("expected indented output, got %q", got)
	}
}

func TestGishPrintArray(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`gish.println([1, 2, 3])`)
	if err != nil {
		t.Fatalf("println array failed: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(got, "[") {
		t.Errorf("expected array output, got %q", got)
	}
}

func TestGishPrintPrimitive(t *testing.T) {
	buf, _ := initTestVM(t)

	buf.Reset()
	jsVM.RunString(`gish.print(42)`)
	if got := buf.String(); got != "42" {
		t.Errorf("number: got %q want %q", got, "42")
	}

	buf.Reset()
	jsVM.RunString(`gish.print(true)`)
	if got := buf.String(); got != "true" {
		t.Errorf("bool: got %q want %q", got, "true")
	}
}

func TestGishExec(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`
		var r = gish.exec("echo", ["hello"]);
		gish.print(r.stdout.trim() + "|" + r.exitCode);
	`)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if got := buf.String(); got != "hello|0" {
		t.Errorf("got %q want %q", got, "hello|0")
	}
}

func TestGishExecFailure(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`
		var r = gish.exec("false", []);
		gish.print(String(r.exitCode));
	`)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if got := buf.String(); got != "1" {
		t.Errorf("got %q want %q", got, "1")
	}
}

func TestGishEnv(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`
		gish.env.set("GISH_JS_TEST", "value42");
		gish.print(gish.env.get("GISH_JS_TEST"));
	`)
	if err != nil {
		t.Fatalf("env failed: %v", err)
	}
	if got := buf.String(); got != "value42" {
		t.Errorf("got %q want %q", got, "value42")
	}

	// Check it's really in the environment
	if v := os.Getenv("GISH_JS_TEST"); v != "value42" {
		t.Errorf("os.Getenv got %q want %q", v, "value42")
	}

	// Unset
	buf.Reset()
	_, err = jsVM.RunString(`
		gish.env.unset("GISH_JS_TEST");
		gish.print(gish.env.get("GISH_JS_TEST"));
	`)
	if err != nil {
		t.Fatalf("env unset failed: %v", err)
	}
	if got := buf.String(); got != "" {
		t.Errorf("got %q want empty", got)
	}

	os.Unsetenv("GISH_JS_TEST")
}

func TestGishEnvAll(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	os.Setenv("GISH_JS_ALL_TEST", "yes")
	defer os.Unsetenv("GISH_JS_ALL_TEST")

	_, err := jsVM.RunString(`
		var all = gish.env.all();
		gish.print(all["GISH_JS_ALL_TEST"]);
	`)
	if err != nil {
		t.Fatalf("env.all failed: %v", err)
	}
	if got := buf.String(); got != "yes" {
		t.Errorf("got %q want %q", got, "yes")
	}
}

func TestGishCwd(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`gish.print(gish.cwd())`)
	if err != nil {
		t.Fatalf("cwd failed: %v", err)
	}
	wd, _ := os.Getwd()
	wdR, _ := filepath.EvalSymlinks(wd)
	gotR, _ := filepath.EvalSymlinks(buf.String())
	if gotR != wdR {
		t.Errorf("got %q want %q", gotR, wdR)
	}
}

func TestGishSpawn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows command-line quoting mangles embedded newlines in arguments")
	}
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`
		var collected = [];
		var r = gish.spawn("printf", ["one\ntwo\nthree\n"], function(line) {
			collected.push(line);
		});
		gish.print(collected.join(",") + "|" + r.exitCode);
	`)
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	if got := buf.String(); got != "one,two,three|0" {
		t.Errorf("got %q want %q", got, "one,two,three|0")
	}
}

func TestGishSpawnStderr(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`
		var r = gish.spawn("sh", ["-c", "echo out; echo err >&2"], function(line) {
			gish.print(line);
		});
		gish.print("|" + r.stderr.trim());
	`)
	if err != nil {
		t.Fatalf("spawn stderr failed: %v", err)
	}
	if got := buf.String(); got != "out|err" {
		t.Errorf("got %q want %q", got, "out|err")
	}
}

func TestGishSpawnNonZeroExit(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`
		var lines = [];
		var r = gish.spawn("sh", ["-c", "echo hello; exit 2"], function(line) {
			lines.push(line);
		});
		gish.print(lines[0] + "|" + r.exitCode);
	`)
	if err != nil {
		t.Fatalf("spawn failed: %v", err)
	}
	if got := buf.String(); got != "hello|2" {
		t.Errorf("got %q want %q", got, "hello|2")
	}
}

func TestGishSpawnBadCallback(t *testing.T) {
	initTestVM(t)

	_, err := jsVM.RunString(`gish.spawn("echo", ["hi"], "not a function")`)
	if err == nil {
		t.Fatal("expected error for non-function callback")
	}
}

func TestGishParseJSON(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`
		var obj = gish.parseJSON('{"name":"gish","ver":1}');
		gish.print(obj.name + "|" + obj.ver);
	`)
	if err != nil {
		t.Fatalf("parseJSON failed: %v", err)
	}
	if got := buf.String(); got != "gish|1" {
		t.Errorf("got %q want %q", got, "gish|1")
	}
}

func TestGishParseJSONError(t *testing.T) {
	initTestVM(t)

	_, err := jsVM.RunString(`gish.parseJSON("not json")`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGishToJSON(t *testing.T) {
	buf, _ := initTestVM(t)
	buf.Reset()

	_, err := jsVM.RunString(`gish.print(gish.toJSON({a: 1}, false))`)
	if err != nil {
		t.Fatalf("toJSON compact failed: %v", err)
	}
	if got := buf.String(); got != `{"a":1}` {
		t.Errorf("got %q want %q", got, `{"a":1}`)
	}

	buf.Reset()
	_, err = jsVM.RunString(`gish.print(gish.toJSON({b: 2}))`)
	if err != nil {
		t.Fatalf("toJSON pretty failed: %v", err)
	}
	if !strings.Contains(buf.String(), "  ") {
		t.Errorf("expected indented output, got %q", buf.String())
	}
}

func TestLoadInitScript(t *testing.T) {
	buf, _ := initTestVM(t)

	dir := t.TempDir()
	t.Setenv("GISH_CONFIG_DIR", dir)

	// Write an init.js that registers a command
	os.WriteFile(filepath.Join(dir, "init.js"), []byte(`
		gish.register("jshello", function(ctx) {
			gish.println("js says hello");
		});
	`), 0644)

	buf.Reset()
	loadInitScript(jsVM, &ExecCtx{In: strings.NewReader(""), Out: buf, ErrOut: buf})

	fn, ok := Builtins["jshello"]
	if !ok {
		t.Fatal("jshello not registered after loadInitScript")
	}

	buf.Reset()
	helloCtx := &ExecCtx{
		In: strings.NewReader(""), Out: buf, ErrOut: buf,
	}
	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "jshello"}},
		Line:   "jshello",
	}
	if err := fn(cmd, helloCtx); err != nil {
		t.Fatalf("jshello error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "js says hello" {
		t.Errorf("got %q want %q", got, "js says hello")
	}

	delete(Builtins, "jshello")
}

func TestLoadInitScriptSyntaxError(t *testing.T) {
	initTestVM(t)

	dir := t.TempDir()
	t.Setenv("GISH_CONFIG_DIR", dir)

	os.WriteFile(filepath.Join(dir, "init.js"), []byte(`function(`), 0644)

	var errBuf bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &errBuf, ErrOut: &errBuf}
	loadInitScript(jsVM, ctx)

	if !strings.Contains(errBuf.String(), "init.js") {
		t.Errorf("expected error mentioning init.js, got %q", errBuf.String())
	}
}

func TestLoadInitScriptMissing(t *testing.T) {
	initTestVM(t)
	t.Setenv("GISH_CONFIG_DIR", "/nonexistent/gish/config/path")

	// Should not panic or error when init.js doesn't exist
	var buf bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &buf, ErrOut: &buf}
	loadInitScript(jsVM, ctx)
}

func TestJSPromptDefault(t *testing.T) {
	initTestVM(t)
	promptFn = nil
	if got := JSPrompt(); got != "gish> " {
		t.Errorf("got %q want %q", got, "gish> ")
	}
}

func TestJSPromptCustom(t *testing.T) {
	initTestVM(t)

	_, err := jsVM.RunString(`gish.setPrompt(function() { return "custom> "; })`)
	if err != nil {
		t.Fatal(err)
	}
	if got := JSPrompt(); got != "custom> " {
		t.Errorf("got %q want %q", got, "custom> ")
	}
}

func TestJSPromptErrorFallback(t *testing.T) {
	initTestVM(t)

	_, err := jsVM.RunString(`gish.setPrompt(function() { throw "boom"; })`)
	if err != nil {
		t.Fatal(err)
	}
	if got := JSPrompt(); got != "gish> " {
		t.Errorf("got %q want %q on error", got, "gish> ")
	}
}

func TestGishSource(t *testing.T) {
	buf, _ := initTestVM(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "lib.js")
	os.WriteFile(path, []byte(`var libLoaded = true; gish.println("lib loaded")`), 0644)

	buf.Reset()
	_, err := jsVM.RunString(fmt.Sprintf(`gish.source(%q)`, path))
	if err != nil {
		t.Fatalf("gish.source error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "lib loaded" {
		t.Errorf("got %q want %q", got, "lib loaded")
	}

	// Verify the sourced file's side effects are visible
	val, err := jsVM.RunString(`libLoaded`)
	if err != nil {
		t.Fatalf("libLoaded check: %v", err)
	}
	if !val.ToBoolean() {
		t.Error("expected libLoaded to be true")
	}
}

func TestGishSourceMissing(t *testing.T) {
	initTestVM(t)

	_, err := jsVM.RunString(`gish.source("/nonexistent/file.js")`)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGishSourceSyntaxError(t *testing.T) {
	initTestVM(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.js")
	os.WriteFile(path, []byte(`function(`), 0644)

	_, err := jsVM.RunString(fmt.Sprintf(`gish.source(%q)`, path))
	if err == nil {
		t.Fatal("expected error for syntax error")
	}
}

func TestSourceFile(t *testing.T) {
	buf, ctx := initTestVM(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "test.js")
	os.WriteFile(path, []byte(`gish.println("sourced")`), 0644)

	buf.Reset()
	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "source"}, {Kind: TokenWord, Value: path}},
		Line:   "source " + path,
	}
	if err := builtinSource(cmd, ctx); err != nil {
		t.Fatalf("source error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "sourced" {
		t.Errorf("got %q want %q", got, "sourced")
	}
}

func TestSourceFileMissing(t *testing.T) {
	_, ctx := initTestVM(t)

	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "source"}, {Kind: TokenWord, Value: "/nonexistent/file.js"}},
		Line:   "source /nonexistent/file.js",
	}
	if err := builtinSource(cmd, ctx); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSourceFileSyntaxError(t *testing.T) {
	_, ctx := initTestVM(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.js")
	os.WriteFile(path, []byte(`function(`), 0644)

	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "source"}, {Kind: TokenWord, Value: path}},
		Line:   "source " + path,
	}
	err := builtinSource(cmd, ctx)
	if err == nil {
		t.Fatal("expected error for syntax error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q should mention file path", err.Error())
	}
}

func TestSourceNoArgs(t *testing.T) {
	_, ctx := initTestVM(t)

	cmd := &Command{
		Tokens: []Token{{Kind: TokenWord, Value: "source"}},
		Line:   "source",
	}
	if err := builtinSource(cmd, ctx); err == nil {
		t.Fatal("expected error for no args")
	}
}

func TestConfigDir(t *testing.T) {
	// GISH_CONFIG_DIR takes priority
	t.Setenv("GISH_CONFIG_DIR", "/custom/config")
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := configDir(); got != "/custom/config" {
		t.Errorf("got %q want /custom/config", got)
	}

	// XDG_CONFIG_HOME is next
	t.Setenv("GISH_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	if got := configDir(); got != filepath.Join("/xdg/config", "gish") {
		t.Errorf("got %q want /xdg/config/gish", got)
	}

	// Falls back to ~/.config/gish
	t.Setenv("GISH_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := configDir()
	if !strings.HasSuffix(dir, filepath.Join(".config", "gish")) {
		t.Errorf("got %q, expected suffix .config/gish", dir)
	}
}
