package main

import (
	"fmt"
	"os"
	"time"

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

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		files, err := getStatus(repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		printStatus(files)
	}
}

func printStatus(files []FileStatus) {
	fmt.Print("\033[H\033[2J")

	if len(files) == 0 {
		fmt.Println("nothing to commit, working tree clean")
		return
	}

	for _, f := range files {
		fmt.Printf("%c%c\t%s\n", f.Staging, f.Worktree, f.Path)
	}
}
