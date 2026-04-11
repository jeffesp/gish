package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func RunREPL(in io.Reader, out io.Writer, err io.Writer, afterCmd func()) {
	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "gish> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			tokens := strings.Fields(line)
			name, args := tokens[0], tokens[1:]
			fn, ok := Builtins[name]
			if ok {
				if err_ := fn(args, out); err_ != nil {
					fmt.Fprintf(err, "error: %v\n", err_)
				}
			} else {
				cmd := exec.Command(name, args...)
				cmd.Stdin = os.Stdin
				cmd.Stdout = out
				cmd.Stderr = err
				SetCurrentCmd(cmd)
				err_ := cmd.Run()
				ClearCurrentCmd()
				if err_ != nil {
					fmt.Fprintf(err, "error: %v\n", err_)
				}
			}
		}

		if afterCmd != nil {
			afterCmd()
		}
	}
}
