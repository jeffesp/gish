package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type BuiltinFunc func(args []string, w io.Writer) error

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
}

func builtinSet(args []string, w io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: set KEY VALUE")
	}
	return os.Setenv(args[0], args[1])
}

func builtinUnset(args []string, w io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: unset KEY")
	}
	return os.Unsetenv(args[0])
}

func builtinEnv(args []string, w io.Writer) error {
	for _, e := range os.Environ() {
		fmt.Fprintln(w, e)
	}
	return nil
}

func builtinCd(args []string, w io.Writer) error {
	var dir string
	switch len(args) {
	case 0:
		dir = os.Getenv("HOME")
	case 1:
		dir = args[0]
	default:
		return fmt.Errorf("usage: cd [dir]")
	}
	return os.Chdir(dir)
}

func builtinClear(args []string, w io.Writer) error {
	fmt.Fprint(w, "\033[H\033[2J")
	return nil
}

func builtinEcho(args []string, w io.Writer) error {
	expanded := make([]string, len(args))
	for i, arg := range args {
		if len(arg) > 1 && arg[0] == '$' {
			expanded[i] = os.Getenv(arg[1:])
		} else {
			expanded[i] = arg
		}
	}
	fmt.Fprintln(w, strings.Join(expanded, " "))
	return nil
}
