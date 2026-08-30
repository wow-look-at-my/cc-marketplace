package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"os"
	"path/filepath"
	"strings"
)

// The rule is scoped to paths, never to commands. A directory a build owns is
// writable by whatever writes it; a source file is not writable by anything but
// the edit tools. Everything below decides which side of that line a path is on.

// guardedRoots are the directories whose content this hook protects: the
// repository the session works in, and the project directory the CLI names.
// Both are used because they disagree in the two shapes that occur -- a session
// rooted directly on one repository, and one rooted on a parent holding several.
func guardedRoots(cwd string) []string {
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if r := repoRoot(p); r != "" {
			p = r
		}
		for _, existing := range roots {
			if existing == p {
				return
			}
		}
		roots = append(roots, p)
	}
	add(cwd)
	add(os.Getenv("CLAUDE_PROJECT_DIR"))
	return roots
}

// repoRoot walks up for a .git entry. A subprocess would answer the same
// question, and this hook runs in front of every Bash call, so it reads the
// directory tree instead of paying for git.
func repoRoot(dir string) string {
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// buildOutputDirs name the directories a build, a package manager or a tool
// cache owns. A path under one of them is writable by anything: nobody reviews
// a compiled artifact, and requiring Edit for one would deny every build.
// The allowance is scoped to these directories and never to the commands that
// write them, so `sed -i` into node_modules passes and `npm` into src does not.
var buildOutputDirs = set.Of[string]("build", "dist", "target", "out",
	"node_modules", "vendor", "coverage",
	".cache", ".venv", "venv", "__pycache__",
	".pytest_cache", ".gradle", ".tox", ".next",
	".parcel-cache", ".turbo", ".terraform")

// protectedConfig names the live settings a session must not rewrite. These sit
// outside every guarded root, so the path rules never reach them: re-granting
// what this hook denies is its own route, closed on its own terms.
// The repository's own plugin sources are not this -- editing
// `plugins/x/.claude-plugin/plugin.json` is ordinary work on source code.
func isProtectedConfig(abs string) bool {
	abs = filepath.Clean(abs)
	base := filepath.Base(abs)
	if base != "settings.json" && base != "settings.local.json" {
		return false
	}
	return filepath.Base(filepath.Dir(abs)) == ".claude"
}

// insideGuarded reports whether a write to abs changes content this hook
// protects, and names the root it belongs to.
func insideGuarded(roots []string, abs string) (string, bool) {
	abs = filepath.Clean(abs)
	for _, root := range roots {
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if rel == "." || isBuildOutput(rel) {
			continue
		}
		return root, true
	}
	return "", false
}

// coversGuarded is insideGuarded's other direction: a write whose target is a
// directory rather than a named file -- an extraction, a patch, a git verb --
// lands somewhere under that directory, so a guarded root sitting inside it is
// just as reachable as one containing it.
func coversGuarded(roots []string, dir string) (string, bool) {
	if root, ok := insideGuarded(roots, dir); ok {
		return root, true
	}
	dir = filepath.Clean(dir)
	for _, root := range roots {
		rel, err := filepath.Rel(dir, root)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return root, true
		}
	}
	return "", false
}

func isBuildOutput(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if buildOutputDirs.Contains(part) {
			return true
		}
	}
	return false
}

// display renders a path the way the message should name it: relative to the
// root it violates, because that is the spelling the reader recognises.
func display(root, abs string) string {
	if rel, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return abs
}
