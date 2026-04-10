package main

import (
	"sort"

	"github.com/go-git/go-git/v5"
)

type FileStatus struct {
	Path    string
	Staging byte
	Worktree byte
}

func getStatus(repo *git.Repository) ([]FileStatus, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}

	status, err := wt.Status()
	if err != nil {
		return nil, err
	}

	files := make([]FileStatus, 0, len(status))
	for path, fs := range status {
		files = append(files, FileStatus{
			Path:     path,
			Staging:  byte(fs.Staging),
			Worktree: byte(fs.Worktree),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}
