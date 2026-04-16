package main

import (
	"os"
)

func main() {
	SetupSignals(os.Stderr)
	ctx := &ExecCtx{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	RunREPL(ctx, func(ctx *ExecCtx) {})
}
