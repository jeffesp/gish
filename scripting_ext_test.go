package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func evalString(t *testing.T, js string) string {
	t.Helper()
	v, err := jsVM.RunString(js)
	if err != nil {
		t.Fatalf("%s: %v", js, err)
	}
	return v.String()
}

func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestFS(t *testing.T) {
	initTestVM(t)
	dir := withTempDir(t, "a.txt", "sub/")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := jsQuote(dir)
	cases := []struct{ js, want string }{
		{`gish.fs.readFile(` + p + ` + "/a.txt")`, "data"},
		{`gish.fs.exists(` + p + ` + "/a.txt") + "|" + gish.fs.exists(` + p + ` + "/nope")`, "true|false"},
		{`gish.fs.readdir(` + p + `).sort().join(",")`, "a.txt,sub"},
		{`var s = gish.fs.stat(` + p + ` + "/sub"); s.isDir + "|" + s.isFile`, "true|false"},
		{`String(gish.fs.stat(` + p + ` + "/nope"))`, "null"},
	}
	for _, c := range cases {
		if got := evalString(t, c.js); got != c.want {
			t.Errorf("%s = %q, want %q", c.js, got, c.want)
		}
	}
	if _, err := jsVM.RunString(`gish.fs.readFile("/definitely/not/here")`); err == nil {
		t.Error("readFile of a missing file should throw")
	}
}

func TestRequire(t *testing.T) {
	initTestVM(t)
	dir := withTempDir(t, "lib/")
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.js", `var h = require("./lib/helper"); module.exports = {v: h.double(21), dir: __dirname};`)
	write("lib/helper.js", `var n = require("./count"); n.hits++; exports.double = function (x) { return x * 2; };`)
	write("lib/count.js", `module.exports = {hits: 0};`)
	write("data.json", `{"k": [1, 2]}`)
	write("bad.js", `throw new Error("boom")`)
	p := jsQuote(dir)

	cases := []struct{ js, want string }{
		{`require(` + p + ` + "/main.js").v`, "42"},
		{`require(` + p + ` + "/main").dir`, dir},
		{`require(` + p + ` + "/lib/count").hits`, "1"}, // cached: helper ran once
		{`require(` + p + ` + "/data.json").k.length`, "2"},
		{`typeof helperLeak`, "undefined"}, // modules do not leak globals
	}
	for _, c := range cases {
		if got := evalString(t, c.js); got != c.want {
			t.Errorf("%s = %q, want %q", c.js, got, c.want)
		}
	}
	for _, js := range []string{
		`require("lodash")`,
		`require(` + p + ` + "/missing")`,
		`require(` + p + ` + "/bad.js")`,
	} {
		if _, err := jsVM.RunString(js); err == nil {
			t.Errorf("%s should throw", js)
		}
	}
	// A failed module is not cached.
	write("bad.js", `module.exports = 1`)
	if got := evalString(t, `require(`+p+` + "/bad.js")`); got != "1" {
		t.Errorf("after fix = %q, want 1", got)
	}
}

func TestCompleteFiles(t *testing.T) {
	initTestVM(t)
	withTempDir(t, "gzzfile", "gzzdir/")
	sep := string(filepath.Separator)
	if got := evalString(t, `gish.completeFiles("gzz").join(",")`); got != "gzzdir"+sep+",gzzfile" {
		t.Errorf("got %q", got)
	}
	if got := evalString(t, `gish.completeFiles("gzz", {dirsOnly: true}).join(",")`); got != "gzzdir"+sep {
		t.Errorf("dirsOnly got %q", got)
	}
}
