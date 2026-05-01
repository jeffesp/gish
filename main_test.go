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
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
