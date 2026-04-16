package main

import "io"

type ExecCtx struct {
	In     io.Reader // input source (type-assert to *os.File where fd is needed)
	Line   string    // raw input line as typed
	Tokens []Token   // parsed tokens (first is command name)
	Out    io.Writer
	ErrOut io.Writer
}

func (ctx *ExecCtx) Args() []Token {
	if len(ctx.Tokens) < 2 {
		return nil
	}
	return ctx.Tokens[1:]
}

func (ctx *ExecCtx) Name() string {
	if len(ctx.Tokens) == 0 {
		return ""
	}
	return ctx.Tokens[0].Value
}
