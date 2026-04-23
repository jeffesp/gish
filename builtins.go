package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type BuiltinFunc func(cmd *Command, ctx *ExecCtx) error

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
	RegisterBuiltin("exit", builtinExit)
}

func builtinSet(cmd *Command, ctx *ExecCtx) error {
	args := cmd.Args()
	if len(args) != 2 {
		return fmt.Errorf("usage: set KEY VALUE")
	}
	return os.Setenv(args[0].Value, args[1].Value)
}

func builtinUnset(cmd *Command, ctx *ExecCtx) error {
	args := cmd.Args()
	if len(args) != 1 {
		return fmt.Errorf("usage: unset KEY")
	}
	return os.Unsetenv(args[0].Value)
}

func builtinEnv(cmd *Command, ctx *ExecCtx) error {
	for _, e := range os.Environ() {
		fmt.Fprintln(ctx.Out, e)
	}
	return nil
}

func builtinCd(cmd *Command, ctx *ExecCtx) error {
	args := cmd.Args()

	var dir string
	switch len(args) {
	case 0:
		dir = os.Getenv("HOME")
	case 1:
		// TODO: support '-' to go back a dir
		dir = args[0].Value
	default:
		return fmt.Errorf("usage: cd [dir]")
	}
	return os.Chdir(dir)
}

func builtinClear(cmd *Command, ctx *ExecCtx) error {
	fmt.Fprint(ctx.Out, "\033[H\033[2J")
	return nil
}

func builtinExec(cmd *Command, ctx *ExecCtx) error {
	args := cmd.Args()
	if len(args) == 0 {
		return fmt.Errorf("usage: exec command [args...]")
	}
	bin, err := exec.LookPath(args[0].Value)
	if err != nil {
		return err
	}
	argv := tokenValues(args)
	if ctx.RestoreTerm != nil {
		ctx.RestoreTerm()
	}
	return syscall.Exec(bin, argv, os.Environ())
}

func builtinEcho(cmd *Command, ctx *ExecCtx) error {
	fmt.Fprintln(ctx.Out, strings.Join(tokenValues(cmd.Args()), " "))
	return nil
}

func builtinExit(cmd *Command, _ctx *ExecCtx) error {
	if len(cmd.Args()) > 1 {
		return fmt.Errorf("usage: exit [int]")
	}
	if len(cmd.Args()) == 1 {
		code, e := strconv.Atoi(cmd.Args()[0].Value)
		if e != nil {
			return e
		}
		os.Exit(code)
	}
	os.Exit(0)
	return nil
}
