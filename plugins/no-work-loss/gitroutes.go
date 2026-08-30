package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"os"
	"strings"
)

// git is a legitimate thing for Bash to run, and most of it stays out of this
// hook's way: status, log, diff, add, commit, push, fetch, branch, tag, and
// creating or switching a branch all leave file content to the edit tools.
//
// What is closed is git used as an editor -- a verb that puts content into the
// working tree or the index without any tool call showing it -- and the plumbing
// that skips the worktree entirely by writing a blob and pointing the index at
// it.

// worktreeVerbs put committed content into the tree.
var worktreeVerbs = map[string]string{
	"restore":     "git restore",
	"stash":       "git stash pop",
	"revert":      "git revert",
	"cherry-pick": "git cherry-pick",
	"merge":       "git merge",
	"rebase":      "git rebase",
	"pull":        "git pull",
	"am":          "git am",
	"apply":       "git apply",
	"checkout":    "git checkout",
	"reset":       "git reset",
}

// plumbingVerbs write objects, the index or refs directly. `git hash-object -w`
// followed by `git update-index --cacheinfo` produces a committed change that
// never existed as a file, which is the same act as editing one.
var plumbingVerbs = map[string]string{
	"hash-object":    "git hash-object -w",
	"update-index":   "git update-index",
	"commit-tree":    "git commit-tree",
	"update-ref":     "git update-ref",
	"fast-import":    "git fast-import",
	"mktree":         "git mktree",
	"read-tree":      "git read-tree",
	"checkout-index": "git checkout-index",
	"symbolic-ref":   "git symbolic-ref",
}

// gitValueOptions are the global options that take a separate value, so the verb
// is found rather than mistaken for one of their arguments.
var gitValueOptions = set.Of[string]("-C", "-c", "--git-dir", "--work-tree",
	"--namespace", "--exec-path", "--config-env")

func gitWrites(seg segment, name string, rest []word) ([]write, bool) {
	if name != "git" {
		return nil, false
	}
	dir := seg.cwd
	relocated := seg.relocated
	verb := ""
	var args []word
	for i := 0; i < len(rest); i++ {
		t := rest[i].text
		if !strings.HasPrefix(t, "-") {
			verb, args = t, rest[i+1:]
			break
		}
		if t == "-C" && i+1 < len(rest) {
			i++
			if !rest[i].static {
				relocated = true
				continue
			}
			dir = abs(dir, rest[i].text)
			continue
		}
		if t == "--git-dir" || t == "--work-tree" || strings.HasPrefix(t, "--git-dir=") || strings.HasPrefix(t, "--work-tree=") {
			relocated = true
		}
		if gitValueOptions.Contains(t) && i+1 < len(rest) {
			i++
		}
	}
	if verb == "" {
		return nil, true
	}

	route, hit := plumbingVerbs[verb]
	if !hit {
		route, hit = worktreeVerbs[verb]
	}
	if !hit || !gitVerbWrites(verb, args, dir) {
		return nil, true
	}
	if relocated {
		return []write{{route: route, opaque: "a git command whose repository is relocated by an option or an environment variable, so where it writes is not in the command text"}}, true
	}
	return []write{{route: route, dir: dir, whole: true}}, true
}

// gitVerbWrites narrows the verbs that only sometimes put content in the tree.
// Getting this wrong in either direction costs: denying `git checkout -b` breaks
// the branch every session is required to create, and allowing `git checkout
// master -- src/` hands over a whole directory of content with no tool call.
func gitVerbWrites(verb string, args []word, dir string) bool {
	flags, operands := scanArgs(args, set.Of[string]("-m", "--message", "-b", "-B", "-c", "-C", "--onto", "--strategy", "-s", "-X"))
	dashDash := false
	for _, a := range args {
		if a.text == "--" {
			dashDash = true
		}
	}
	has := func(names ...string) bool {
		for _, n := range names {
			if _, ok := flags[n]; ok {
				return true
			}
		}
		return false
	}
	// --abort and --quit put back the state the operation started from, and
	// --continue/--skip carry on one already under way. The content decision was
	// made when it started, which is the invocation this rule is aimed at.
	if has("--abort", "--quit", "--continue", "--skip") {
		return false
	}

	switch verb {
	case "checkout":
		// A pathspec is the editing form. A bare ref switch replaces the tree
		// with a commit git already holds, which is branch navigation rather than
		// authored content, and every session is told to work on a branch.
		if has("-b", "-B", "--orphan", "--detach", "--track", "-t") {
			return false
		}
		return dashDash || len(operands) > 1 || namesExistingPath(dir, operands)
	case "restore":
		// --staged alone moves the index back to HEAD and leaves the file on
		// disk untouched; anything else rewrites the file.
		return !has("--staged") || has("--worktree", "-W")
	case "stash":
		return len(operands) > 0 && (operands[0].text == "pop" || operands[0].text == "apply")
	case "reset":
		// A soft or mixed reset moves refs and the index; only these three
		// rewrite the files on disk.
		return has("--hard", "--merge", "--keep")
	case "update-ref":
		// Setting a ref is the last step of the plumbing route -- hash a blob,
		// build a tree, point a ref at it -- so it introduces content. Deleting
		// one introduces none, and the destruction half already judges whether
		// the commits it drops survive elsewhere.
		return !has("-d", "--delete")
	}
	return true
}

// namesExistingPath separates `git checkout master` from `git checkout src/` by
// asking the filesystem rather than guessing from the spelling -- a tag called
// v1.0 looks exactly like a path and is not one.
func namesExistingPath(dir string, operands []word) bool {
	for _, o := range operands {
		if !o.static {
			return true // unknowable, and unknowable denies
		}
		p := abs(dir, o.text)
		if p == "" {
			return true
		}
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	return false
}
