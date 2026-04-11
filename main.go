package main

import (
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
)

func main() {
	path := "."
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening repo: %v\n", err)
		os.Exit(1)
	}

	SetupSignals(os.Stderr)
	RunREPL(repo, os.Stdin, os.Stdout, os.Stderr)
}
