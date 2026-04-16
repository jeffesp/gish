package main

import (
	"bytes"
	"os"
	"path/filepath"
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
		Tokens: []Token{{TokenWord, "js"}, {TokenWord, "2+2"}},
		Line:   "js 2+2",
	}
	if err := builtinJS(ctx); err != nil {
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
		Tokens: []Token{{TokenWord, "js"}, {TokenWord, "2"}, {TokenWord, "+"}, {TokenWord, "3"}},
		Line:   "js 2 + 3",
	}
	if err := builtinJS(ctx); err != nil {
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
		Tokens: []Token{{TokenWord, "js"}},
		Line:   "js",
	}
	if err := builtinJS(ctx); err == nil {
		t.Error("expected error for no args")
	}
}

func TestJSException(t *testing.T) {
	initTestVM(t)

	ctx := &ExecCtx{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{},
		Tokens: []Token{{TokenWord, "js"}, {TokenWord, "throw"}, {TokenWord, "new"}, {TokenWord, `Error("boom")`}},
		Line:   `js throw new Error("boom")`,
	}
	err := builtinJS(ctx)
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
		Tokens: []Token{{TokenWord, "greet"}, {TokenWord, "world"}},
		Line:   "greet world",
	}
	if err := fn(greetCtx); err != nil {
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
		Tokens: []Token{{TokenWord, "ret"}},
		Line:   "ret",
	}
	if err := fn(retCtx); err != nil {
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

func TestLoadScripts(t *testing.T) {
	buf, _ := initTestVM(t)

	dir := t.TempDir()
	t.Setenv("GISH_SCRIPTS_DIR", dir)

	// Write a script that registers a command
	os.WriteFile(filepath.Join(dir, "01-hello.js"), []byte(`
		gish.register("jshello", function(ctx) {
			gish.println("js says hello");
		});
	`), 0644)

	// Re-load scripts
	buf.Reset()
	loadScripts(jsVM, &ExecCtx{In: strings.NewReader(""), Out: buf, ErrOut: buf})

	fn, ok := Builtins["jshello"]
	if !ok {
		t.Fatal("jshello not registered after loadScripts")
	}

	buf.Reset()
	helloCtx := &ExecCtx{
		In: strings.NewReader(""), Out: buf, ErrOut: buf,
		Tokens: []Token{{TokenWord, "jshello"}},
		Line:   "jshello",
	}
	if err := fn(helloCtx); err != nil {
		t.Fatalf("jshello error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "js says hello" {
		t.Errorf("got %q want %q", got, "js says hello")
	}

	delete(Builtins, "jshello")
}

func TestLoadScriptsSyntaxError(t *testing.T) {
	initTestVM(t)

	dir := t.TempDir()
	t.Setenv("GISH_SCRIPTS_DIR", dir)

	os.WriteFile(filepath.Join(dir, "bad.js"), []byte(`function(`), 0644)
	os.WriteFile(filepath.Join(dir, "good.js"), []byte(`gish.register("goodcmd", function() {})`), 0644)

	var errBuf bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &errBuf, ErrOut: &errBuf}
	loadScripts(jsVM, ctx)

	// Bad script should produce error output
	if !strings.Contains(errBuf.String(), "bad.js") {
		t.Errorf("expected error for bad.js, got %q", errBuf.String())
	}

	// Good script should still load
	if _, ok := Builtins["goodcmd"]; !ok {
		t.Error("goodcmd not registered — good script didn't load after bad one")
	}

	delete(Builtins, "goodcmd")
}

func TestLoadScriptsMissingDir(t *testing.T) {
	initTestVM(t)
	t.Setenv("GISH_SCRIPTS_DIR", "/nonexistent/gish/scripts/path")

	// Should not panic or error
	var buf bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &buf, ErrOut: &buf}
	loadScripts(jsVM, ctx)
}

func TestScriptsDir(t *testing.T) {
	// With env var set
	t.Setenv("GISH_SCRIPTS_DIR", "/custom/scripts")
	if got := scriptsDir(); got != "/custom/scripts" {
		t.Errorf("got %q want /custom/scripts", got)
	}

	// Without env var
	t.Setenv("GISH_SCRIPTS_DIR", "")
	dir := scriptsDir()
	if !strings.HasSuffix(dir, filepath.Join(".config", "gish", "scripts")) {
		t.Errorf("got %q, expected suffix .config/gish/scripts", dir)
	}
}
