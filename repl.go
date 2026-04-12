package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

type readWriter struct {
	io.Reader
	io.Writer
}

func RunREPL(in io.Reader, out io.Writer, errOut io.Writer, afterCmd func(io.Writer, io.Writer)) {
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		runRawREPL(f, out, errOut, afterCmd)
		return
	}
	runScannerREPL(in, out, errOut, afterCmd)
}

func execLine(line string, out io.Writer, errOut io.Writer, runCmd func(*exec.Cmd) error) {
	tokens := strings.Fields(line)
	name, args := tokens[0], tokens[1:]
	if fn, ok := Builtins[name]; ok {
		if err := fn(args, out); err != nil {
			fmt.Fprintf(errOut, "error: %v\n", err)
		}
		return
	}
	cmd := exec.Command(name, args...)
	SetCurrentCmd(cmd)
	if err := runCmd(cmd); err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
	}
	ClearCurrentCmd()
}

func runRawREPL(in *os.File, out io.Writer, errOut io.Writer, afterCmd func(io.Writer, io.Writer)) {
	fd := int(in.Fd())
	origState, err := term.MakeRaw(fd)
	if err != nil {
		runScannerREPL(in, out, errOut, afterCmd)
		return
	}
	defer term.Restore(fd, origState)

	t := term.NewTerminal(readWriter{in, out}, "gish> ")

	if w, h, err := term.GetSize(fd); err == nil {
		t.SetSize(w, h)
	}

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			if w, h, err := term.GetSize(fd); err == nil {
				t.SetSize(w, h)
			}
		}
	}()

	for {
		line, err := t.ReadLine()
		if err != nil {
			break
		}

		if line = strings.TrimSpace(line); line != "" {
			execLine(line, t, t, func(cmd *exec.Cmd) error {
				cmd.Stdin = in
				cmd.Stdout = out
				cmd.Stderr = errOut
				term.Restore(fd, origState)
				err := cmd.Run()
				term.MakeRaw(fd) //nolint:errcheck
				return err
			})
		}

		if afterCmd != nil {
			afterCmd(t, t)
		}
	}
}

func runScannerREPL(in io.Reader, out io.Writer, errOut io.Writer, afterCmd func(io.Writer, io.Writer)) {
	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "gish> ")
		if !scanner.Scan() {
			break
		}

		if line := strings.TrimSpace(scanner.Text()); line != "" {
			execLine(line, out, errOut, func(cmd *exec.Cmd) error {
				cmd.Stdin = in
				cmd.Stdout = out
				cmd.Stderr = errOut
				return cmd.Run()
			})
		}

	}
}
