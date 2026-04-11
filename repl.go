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

func RunREPL(repo *git.Repository, in io.Reader, out io.Writer, err io.Writer) {
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

		files, err_ := getStatus(repo)
		if err_ != nil {
			fmt.Fprintf(err, "status error: %v\n", err_)
			continue
		}
		printStatus(files)
	}
}
