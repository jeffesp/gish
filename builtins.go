package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type BuiltinFunc func(args []Token, w io.Writer) error

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

func builtinSet(args []Token, w io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: set KEY VALUE")
	}
	return os.Setenv(args[0].Value, args[1].Value)
}

func builtinUnset(args []Token, w io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: unset KEY")
	}
	return os.Unsetenv(args[0].Value)
}

func builtinEnv(args []Token, w io.Writer) error {
	for _, e := range os.Environ() {
		fmt.Fprintln(w, e)
	}
	return nil
}

func builtinCd(args []Token, w io.Writer) error {
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

func builtinClear(args []Token, w io.Writer) error {
	fmt.Fprint(w, "\033[H\033[2J")
	return nil
}

func builtinEcho(args []Token, w io.Writer) error {
	expanded := make([]string, len(args))
	for i, arg := range args {
		if arg.Kind == TokenSingleQuoted {
			expanded[i] = arg.Value
		} else if len(arg.Value) > 1 && arg.Value[0] == '$' {
			expanded[i] = os.Getenv(arg.Value[1:])
		} else {
			expanded[i] = arg.Value
		}
	}
	fmt.Fprintln(w, strings.Join(expanded, " "))
	return nil
}
