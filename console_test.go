package main

import (
	"bytes"
	"strings"
	"testing"
)

// runConsole evaluates src in a fresh VM and returns stdout and stderr.
func runConsole(t *testing.T, src string) (stdout, stderr string) {
	t.Helper()
	var o, e bytes.Buffer
	ctx := &ExecCtx{In: strings.NewReader(""), Out: &o, ErrOut: &e}
	InitScripting(ctx)
	if _, err := jsVM.RunString(src); err != nil {
		t.Fatalf("js error: %v", err)
	}
	return o.String(), e.String()
}

func TestConsoleStreams(t *testing.T) {
	out, errOut := runConsole(t, `
		console.log("a"); console.info("b"); console.debug("c");
		console.error("d"); console.warn("e");`)
	if out != "a\nb\nc\n" {
		t.Errorf("stdout = %q", out)
	}
	if errOut != "d\ne\n" {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestConsoleFormatting(t *testing.T) {
	tests := []struct{ src, want string }{
		{`console.log("a", 1, true, null, undefined)`, "a 1 true null undefined\n"},
		{`console.log()`, "\n"},
		{`console.log("%s is %d years", "Bob", 42)`, "Bob is 42 years\n"},
		{`console.log("%i %f", 3.9, "1.5")`, "3 1.5\n"},
		{`console.log("100%", "done")`, "100% done\n"},
		{`console.log("%%s %s", "x")`, "%s x\n"},
		{`console.log("%s %s", "only")`, "only %s\n"},
		{`console.log("%j", {a: 1})`, "{\"a\":1}\n"},
		{`console.log("%c styled", "color: red")`, " styled\n"},
		{`console.log("x", "%s", "y")`, "x %s y\n"},
		{`console.log({a: 1})`, "{\n  \"a\": 1\n}\n"},
		{`console.log([1, 2], "z")`, "[\n  1,\n  2\n] z\n"},
		{`console.dir({a: 1})`, "{\n  \"a\": 1\n}\n"},
	}
	for _, tt := range tests {
		out, _ := runConsole(t, tt.src)
		if out != tt.want {
			t.Errorf("%s\n got %q\nwant %q", tt.src, out, tt.want)
		}
	}
}

func TestConsoleAssert(t *testing.T) {
	out, errOut := runConsole(t, `
		console.assert(true, "no");
		console.assert(false, "bad %s", "thing");
		console.assert(0);`)
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if want := "Assertion failed: bad thing\nAssertion failed\n"; errOut != want {
		t.Errorf("stderr = %q want %q", errOut, want)
	}
}

func TestConsoleCount(t *testing.T) {
	out, _ := runConsole(t, `
		console.count(); console.count(); console.count("x");
		console.countReset(); console.count();`)
	if want := "default: 1\ndefault: 2\nx: 1\ndefault: 1\n"; out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestConsoleGroup(t *testing.T) {
	out, errOut := runConsole(t, `
		console.group("outer");
		console.log("a\nb");
		console.group();
		console.error("deep");
		console.groupEnd();
		console.groupEnd();
		console.groupEnd();
		console.log("flat");`)
	if want := "outer\n  a\n  b\nflat\n"; out != want {
		t.Errorf("stdout = %q want %q", out, want)
	}
	if want := "    deep\n"; errOut != want {
		t.Errorf("stderr = %q want %q", errOut, want)
	}
}

func TestConsoleTime(t *testing.T) {
	out, errOut := runConsole(t, `
		console.time("t"); console.timeLog("t", "extra"); console.timeEnd("t");
		console.timeEnd("t");`)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "t: ") || !strings.HasSuffix(lines[0], " extra") ||
		!strings.HasPrefix(lines[1], "t: ") || !strings.HasSuffix(lines[1], "ms") {
		t.Errorf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "No such label 't'") {
		t.Errorf("stderr = %q", errOut)
	}
}
