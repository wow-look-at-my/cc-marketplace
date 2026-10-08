package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var separator = regexp.MustCompile(`&&|\|\||[;|\n]`)

// commitDirs gives the directory of each `git commit` in a shell command.
// It follows a `cd DIR` statement and a `git -C DIR` option.
func commitDirs(command, cwd string) []string {
	var dirs []string
	dir := cwd
	for _, stmt := range separator.Split(command, -1) {
		f := strings.Fields(strings.TrimLeft(stmt, "( "))
		if len(f) == 0 {
			continue
		}
		if f[0] == "cd" && len(f) > 1 {
			dir = resolve(dir, unquote(f[1]))
			continue
		}
		if f[0] != "git" {
			continue
		}
		gitDir := dir
		for i := 1; i < len(f); i++ {
			switch {
			case f[i] == "-C" && i+1 < len(f):
				gitDir = resolve(gitDir, unquote(f[i+1]))
				i++
			case f[i] == "-c" && i+1 < len(f):
				i++
			case strings.HasPrefix(f[i], "-"):
			case f[i] == "commit":
				dirs = append(dirs, gitDir)
				i = len(f)
			default:
				i = len(f)
			}
		}
	}
	return dirs
}

func resolve(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

func unquote(s string) string {
	return strings.Trim(s, `"'`)
}

// checkCommit formats each staged or modified tracked .rs file in the repo
// at dir. It returns one line per file that rustfmt changed or rejected.
func checkCommit(dir string) []string {
	root, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	root = strings.TrimSpace(root)
	staged, _ := git(root, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR", "--", "*.rs")
	modified, _ := git(root, "diff", "--name-only", "-z", "--diff-filter=ACMR", "--", "*.rs")
	seen := set.New[string]()
	var reasons []string
	for _, name := range strings.Split(staged+modified, "\x00") {
		if name == "" || seen.Contains(name) {
			continue
		}
		seen.Add(name)
		path := filepath.Join(root, name)
		changed, err := formatFile(path)
		switch {
		case err != nil:
			reasons = append(reasons, path+": rustfmt failed: "+err.Error())
		case changed:
			reasons = append(reasons, path+": rustfmt reformatted it")
		}
	}
	return reasons
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}
