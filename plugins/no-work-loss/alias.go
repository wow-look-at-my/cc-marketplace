package main

import "strings"

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
	if !ok || g.verb == "" || gitBuiltins[g.verb] {
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
var gitBuiltins = map[string]bool{
	"add": true, "am": true, "annotate": true, "apply": true, "archive": true,
	"bisect": true, "blame": true, "branch": true, "bundle": true, "cat-file": true,
	"check-ignore": true, "checkout": true, "checkout-index": true, "cherry": true,
	"cherry-pick": true, "clean": true, "clone": true, "commit": true, "config": true,
	"count-objects": true, "describe": true, "diff": true, "diff-tree": true,
	"difftool": true, "fetch": true, "filter-branch": true, "for-each-ref": true,
	"format-patch": true, "fsck": true, "gc": true, "grep": true, "help": true,
	"init": true, "log": true, "ls-files": true, "ls-remote": true, "ls-tree": true,
	"merge": true, "merge-base": true, "mergetool": true, "mv": true, "notes": true,
	"pull": true, "push": true, "range-diff": true, "rebase": true, "reflog": true,
	"remote": true, "repack": true, "replace": true, "reset": true, "restore": true,
	"rev-list": true, "rev-parse": true, "revert": true, "rm": true, "shortlog": true,
	"show": true, "show-ref": true, "sparse-checkout": true, "stash": true,
	"status": true, "submodule": true, "switch": true, "symbolic-ref": true,
	"tag": true, "update-index": true, "update-ref": true, "var": true,
	"verify-commit": true, "version": true, "whatchanged": true, "worktree": true,
}
