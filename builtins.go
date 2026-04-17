package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

type BuiltinFunc func(ctx *ExecCtx) error

var Builtins = map[string]BuiltinFunc{}

func RegisterBuiltin(name string, fn BuiltinFunc) {
	Builtins[name] = fn
}

func init() {
	RegisterBuiltin("set", builtinSet)
	RegisterBuiltin("unset", builtinUnset)
	RegisterBuiltin("env", builtinEnv)
	RegisterBuiltin("clear", builtinClear)
	RegisterBuiltin("echo", builtinEcho)
	RegisterBuiltin("cd", builtinCd)
	RegisterBuiltin("exec", builtinExec)
}

func builtinSet(ctx *ExecCtx) error {
	args := ctx.Args()
	if len(args) != 2 {
		return fmt.Errorf("usage: set KEY VALUE")
	}
	return os.Setenv(args[0].Value, args[1].Value)
}

func builtinUnset(ctx *ExecCtx) error {
	args := ctx.Args()
	if len(args) != 1 {
		return fmt.Errorf("usage: unset KEY")
	}
	return os.Unsetenv(args[0].Value)
}

func builtinEnv(ctx *ExecCtx) error {
	for _, e := range os.Environ() {
		fmt.Fprintln(ctx.Out, e)
	}
	return nil
}

func builtinCd(ctx *ExecCtx) error {
	args := ctx.Args()
	var dir string
	switch len(args) {
	case 0:
		dir = os.Getenv("HOME")
	case 1:
		dir = args[0].Value
	default:
		return fmt.Errorf("usage: cd [dir]")
	}
	return os.Chdir(dir)
}

func builtinClear(ctx *ExecCtx) error {
	fmt.Fprint(ctx.Out, "\033[H\033[2J")
	return nil
}

func builtinExec(ctx *ExecCtx) error {
	args := ctx.Args()
	if len(args) == 0 {
		return fmt.Errorf("usage: exec command [args...]")
	}
	bin, err := exec.LookPath(args[0].Value)
	if err != nil {
		return err
	}
	argv := tokenValues(args)
	return syscall.Exec(bin, argv, os.Environ())
}

func builtinEcho(ctx *ExecCtx) error {
	fmt.Fprintln(ctx.Out, strings.Join(tokenValues(ctx.Args()), " "))
	return nil
}
