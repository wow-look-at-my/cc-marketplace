package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"strings"
)

// An alias can put a destructive verb behind a harmless-looking name, so a
// classifier that only reads the words as typed is one `git nuke` away from
// useless. Aliases are read from git itself rather than guessed at.
type aliasResolver struct {
	cache map[string]map[string]string
}

func newAliasResolver() *aliasResolver {
	return &aliasResolver{cache: map[string]map[string]string{}}
}

func (a *aliasResolver) table(dir string) map[string]string {
	// Aliases usually live in ~/.gitconfig, which is readable from anywhere,
	// so an unresolvable directory still gets a useful answer.
	if dir == unknownDirText {
		dir = "."
	}
	if t, ok := a.cache[dir]; ok {
		return t
	}
	t := map[string]string{}
	a.cache[dir] = t
	out, _, err := runGit(dir, "config", "--get-regexp", `^alias\.`)
	if err != nil {
		// git could not read its own config, so git cannot resolve an alias
		// either and the command fails on its own.
		return t
	}
	for _, line := range strings.Split(out, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		t[strings.TrimPrefix(name, "alias.")] = value
	}
	return t
}

// expand turns `git <alias> args` into the segments it really runs. Returns
// nil when the verb is a builtin (git resolves those first and refuses to let
// an alias shadow one) or when no such alias exists.
func (a *aliasResolver) expand(seg segment, depth int) []segment {
	if depth >= maxAliasDepth {
		return nil
	}
	g, ok := parseGit(seg.argv, seg.cwd, seg.relocated)
	if !ok || g.verb == "" || gitBuiltins.Contains(g.verb) {
		return nil
	}
	value, found := a.table(g.dir)[g.verb]
	if !found || value == "" {
		return nil
	}
	rest := argsAfterVerb(seg.argv, g.verb)

	// A `!` alias is a shell command, not a git subcommand, so it gets parsed
	// as shell -- which is also what lets `!git reset --hard` be seen at all.
	if shell, isShell := strings.CutPrefix(value, "!"); isShell {
		text := shell
		if len(rest) > 0 {
			text += " " + shellJoin(rest)
		}
		segs, _, parsed := parseSegments(text, g.dir)
		if !parsed {
			return nil
		}
		return segs
	}

	argv := []word{{text: "git", static: true}}
	for _, fld := range strings.Fields(value) {
		argv = append(argv, word{text: fld, static: true})
	}
	argv = append(argv, rest...)
	return []segment{{argv: argv, cwd: g.dir, relocated: seg.relocated}}
}

const maxAliasDepth = 3

// argsAfterVerb returns the words following the subcommand, so an alias keeps
// the arguments it was called with.
func argsAfterVerb(argv []word, verb string) []word {
	for i, a := range argv {
		if a.text == verb {
			return argv[i+1:]
		}
	}
	return nil
}

// Verbs git resolves itself. Listed only to skip a config read on the common
// path -- a name missing from here costs one `git config` call, never a wrong
// verdict.
var gitBuiltins = set.Of[string]("add", "am", "annotate", "apply", "archive",
	"bisect", "blame", "branch", "bundle", "cat-file",
	"check-ignore", "checkout", "checkout-index", "cherry",
	"cherry-pick", "clean", "clone", "commit", "config",
	"count-objects", "describe", "diff", "diff-tree",
	"difftool", "fetch", "filter-branch", "for-each-ref",
	"format-patch", "fsck", "gc", "grep", "help",
	"init", "log", "ls-files", "ls-remote", "ls-tree",
	"merge", "merge-base", "mergetool", "mv", "notes",
	"pull", "push", "range-diff", "rebase", "reflog",
	"remote", "repack", "replace", "reset", "restore",
	"rev-list", "rev-parse", "revert", "rm", "shortlog",
	"show", "show-ref", "sparse-checkout", "stash",
	"status", "submodule", "switch", "symbolic-ref",
	"tag", "update-index", "update-ref", "var",
	"verify-commit", "version", "whatchanged", "worktree")
