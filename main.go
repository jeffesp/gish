package main

import (
	"fmt"
	"os"
	"strconv"

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
	RunREPL(os.Stdin, os.Stdout, os.Stderr, func() {
		if !gitStatusEnabled() {
			return
		}
		files, err := getStatus(repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "status error: %v\n", err)
			return
		}
		printStatus(files)
	})
}

func gitStatusEnabled() bool {
	val, set := os.LookupEnv("GISH_GIT_STATUS")
	if !set {
		return true
	}
	show, err := strconv.ParseBool(val)
	if err != nil {
		return true
	}
	return show
}
