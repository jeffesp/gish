package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/go-git/go-git/v5"
)

func main() {
	SetupSignals(os.Stderr)
	RunREPL(os.Stdin, os.Stdout, os.Stderr, func() {
		if !gitStatusEnabled() {
			return
		}
		path, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to get current path: %v\n", err)
			return
		}
		repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{DetectDotGit: true})
		if err != nil {
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
