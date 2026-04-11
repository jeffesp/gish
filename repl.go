package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5"
)

func RunREPL(repo *git.Repository, in io.Reader, out io.Writer) {
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
				if err := fn(args, out); err != nil {
					fmt.Fprintf(out, "error: %v\n", err)
				}
			} else {
				cmd := exec.Command(name, args...)
				cmd.Stdin = os.Stdin
				cmd.Stdout = out
				cmd.Stderr = out
				if err := cmd.Run(); err != nil {
					fmt.Fprintf(out, "error: %v\n", err)
				}
			}
		}

		files, err := getStatus(repo)
		if err != nil {
			fmt.Fprintf(out, "status error: %v\n", err)
			continue
		}
		printStatus(files)
	}
}
