package main

import (
	"fmt"
	"os"
)

// Version is stamped at build time, e.g.:
//
//	go build -ldflags "-X main.Version=v1.2.3"
var Version = "dev"

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "--version" {
			fmt.Fprintln(os.Stdout, "gish "+Version)
			return
		}
	}
	SetupSignals(os.Stderr)
	ctx := &ExecCtx{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	RunREPL(ctx)
}
