package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "gish-test-*")
	if err != nil {
		panic(err)
	}
	historyFile = filepath.Join(tmp, "history")
	// Tests never write JS traces; trace_test.go re-enables them per test.
	os.Setenv("GISH_TRACES", "0")
	os.Setenv("GISH_TRACE_DIR", filepath.Join(tmp, "traces"))
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
