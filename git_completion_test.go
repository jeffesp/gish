package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// gitRepoFixture creates a repository with commits, branches, a tag, a remote,
// tracked files and untracked files, chdirs into it, and loads the git
// completion script. It returns a function that completes a line the way Tab
// would and returns the candidates (a trailing "·" marks one that takes no
// trailing space).
func gitRepoFixture(t *testing.T) func(line string) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture relies on POSIX paths (GIT_CONFIG_GLOBAL=/dev/null)")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	script, err := filepath.Abs("js/completion/git.js")
	if err != nil {
		t.Fatal(err)
	}
	dir := withTempDir(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// GIT_CONFIG_GLOBAL applies to the shell's own git calls too.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	run("init", "-q", "-b", "main")
	write("README.md", "hi\n")
	write("src/main.go", "package main\n")
	write("src/util.go", "package main\n")
	write("docs/guide.md", "guide\n")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	run("branch", "feature/login")
	run("branch", "fix-bug")
	run("tag", "v1.0")
	run("remote", "add", "origin", "https://example.invalid/repo.git")
	run("update-ref", "refs/remotes/origin/main", "HEAD")
	run("update-ref", "refs/remotes/origin/only-remote", "HEAD")
	write("README.md", "changed\n")  // modified
	write("untracked.txt", "new\n")  // untracked
	write("src/newfile.go", "new\n") // untracked in a tracked dir

	initTestVM(t)
	if _, err := jsVM.RunString(`require(` + jsQuote(script) + `)`); err != nil {
		t.Fatalf("loading git completion: %v", err)
	}
	return func(line string) []string {
		t.Helper()
		matches, noSpace, ok := scriptCandidates(line, wordAtCursor(line, len(line)))
		if !ok {
			return nil
		}
		var out []string
		for _, m := range matches {
			if noSpace[m] {
				m += "·"
			}
			out = append(out, m)
		}
		return out
	}
}

func hasAll(got []string, want ...string) (missing string, ok bool) {
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return w, false
		}
	}
	return "", true
}

func TestGitCompletion(t *testing.T) {
	complete := gitRepoFixture(t)

	cases := []struct {
		line string
		want []string // all must be present
		not  []string // none may be present
	}{
		{"git che", []string{"checkout", "cherry-pick"}, []string{"commit"}},
		{"git --no-p", []string{"--no-pager"}, nil},
		{"git checkout ", []string{"main", "feature/login", "fix-bug", "v1.0", "only-remote", "HEAD"}, nil},
		{"git checkout fe", []string{"feature/login"}, []string{"main"}},
		{"git checkout --det", []string{"--detach"}, nil},
		{"git checkout -b new ", []string{"main", "fix-bug"}, nil},
		{"git switch ", []string{"main", "feature/login", "only-remote"}, []string{"v1.0"}},
		{"git branch -d ", []string{"main", "fix-bug"}, []string{"v1.0", "origin/main"}},
		{"git merge fix", []string{"fix-bug"}, nil},
		{"git add ", []string{"README.md", "untracked.txt", "src/·"}, []string{"docs/·"}},
		{"git add src/", []string{"src/newfile.go"}, []string{"src/main.go"}},
		{"git rm ", []string{"README.md", "src/·", "docs/·"}, []string{"untracked.txt"}},
		{"git rm src/", []string{"src/main.go", "src/util.go"}, nil},
		{"git commit --all", []string{"--all"}, nil},
		{"git commit --am", []string{"--amend"}, nil},
		{"git commit --cleanup=s", []string{"--cleanup=scissors", "--cleanup=strip"}, nil},
		{"git commit -C ", []string{"main", "HEAD"}, nil},
		{"git diff --diff-algorithm=p", []string{"--diff-algorithm=patience"}, nil},
		{"git diff HEAD~1..ma", []string{"HEAD~1..main"}, nil},
		{"git log main...fix", []string{"main...fix-bug"}, nil},
		{"git show HEAD:sr", []string{"HEAD:src/·"}, nil},
		{"git show HEAD:src/ma", []string{"HEAD:src/main.go"}, nil},
		{"git log --pretty=on", []string{"--pretty=oneline"}, nil},
		{"git push ", []string{"origin"}, nil},
		{"git push origin ", []string{"main", "feature/login"}, nil},
		{"git pull origin ", []string{"main", "only-remote"}, nil},
		{"git fetch origin ma", []string{"main:main"}, nil},
		{"git remote ", []string{"add", "remove", "show"}, nil},
		{"git remote show ", []string{"origin"}, nil},
		{"git tag -d ", []string{"v1.0"}, []string{"main"}},
		{"git stash ", []string{"push", "pop", "list"}, nil},
		{"git config user.n", []string{"user.name"}, nil},
		{"git config core.", []string{"core.editor"}, nil},
		{"git config branch.", []string{"branch.main.·", "branch.autoSetupMerge"}, nil},
		{"git config --get remote.origin.u", []string{"remote.origin.url"}, nil},
		{"git -c color.u", []string{"color.ui=·"}, nil},
		{"git reset --ha", []string{"--hard"}, nil},
		{"git status ", []string{"README.md", "untracked.txt", "src/·"}, nil},
		{"git status --untracked-files=n", []string{"--untracked-files=no", "--untracked-files=normal"}, nil},
		{"git stash show --", []string{"--stat"}, nil},
		{"git mv README.md ", []string{"docs/·", "untracked.txt"}, nil},
		{"git restore ", []string{"README.md"}, []string{"untracked.txt", "docs/·"}},
		{"git restore --source=ma", []string{"--source=main"}, nil},
		{"git rebase --onto=fe", []string{"--onto=feature/login"}, nil},
		{"git worktree ", []string{"add", "remove", "list"}, nil},
	}
	for _, c := range cases {
		got := complete(c.line)
		if missing, ok := hasAll(got, c.want...); !ok {
			t.Errorf("%q: missing %q in %v", c.line, missing, got)
		}
		for _, n := range c.not {
			for _, g := range got {
				if g == n {
					t.Errorf("%q: unexpected %q in %v", c.line, n, got)
				}
			}
		}
	}
}

func TestGitCompletionNoSpaceAndFallback(t *testing.T) {
	complete := gitRepoFixture(t)

	// Directories and `--opt=` stay open; plain files and refs take a space.
	got := complete("git add s")
	if m, ok := hasAll(got, "src/·"); !ok {
		t.Errorf("git add s = %v, missing %q", got, m)
	}
	got = complete("git log --pretty=")
	if len(got) == 0 {
		t.Error("git log --pretty= should complete formats")
	}
	// Nothing git-specific to offer: gish falls back to files.
	if got := complete("git clone --depth "); got != nil {
		t.Errorf("git clone --depth = %v, want fallback (nil)", got)
	}
	// Outside any repository there are no refs; fall back to files.
	t.Chdir(t.TempDir())
	if got := complete("git checkout "); got != nil {
		t.Errorf("git checkout outside a repo = %v, want fallback (nil)", got)
	}
}

func TestGitCompletionGlobalOptions(t *testing.T) {
	complete := gitRepoFixture(t)
	repo, _ := os.Getwd()

	// -C directs every git call at another directory.
	t.Chdir(t.TempDir())
	if m, ok := hasAll(complete("git -C "+repo+" checkout fe"), "feature/login"); !ok {
		t.Errorf("git -C <repo> checkout fe: missing %q", m)
	}
	if got := complete("git checkout fe"); got != nil {
		t.Errorf("without -C outside a repo = %v, want nil", got)
	}
	if m, ok := hasAll(complete("git --git-dir="+filepath.Join(repo, ".git")+" checkout fe"), "feature/login"); !ok {
		t.Errorf("--git-dir: missing %q", m)
	}
}

func TestGitCompletionAlias(t *testing.T) {
	complete := gitRepoFixture(t)
	for _, args := range [][]string{{"config", "alias.co", "checkout"}, {"config", "alias.sw", "!git switch"}} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	if m, ok := hasAll(complete("git co fe"), "feature/login"); !ok {
		t.Errorf("git alias co: missing %q", m)
	}
	if m, ok := hasAll(complete("git co"), "co"); !ok {
		t.Errorf("alias names in command list: missing %q", m)
	}
	// A gish alias for git reaches the same completer.
	if _, err := jsVM.RunString(`gish.alias("gg", "git")`); err != nil {
		t.Fatal(err)
	}
	if m, ok := hasAll(complete("gg checkout fe"), "feature/login"); !ok {
		t.Errorf("gish alias gg: missing %q", m)
	}
}
