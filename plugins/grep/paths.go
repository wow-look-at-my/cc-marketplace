// paths.go carries the path resolution/relativization helpers and the
// did-you-mean suggester, copied from the sibling glob plugin (they port
// the same claude-code internals: Vq/QZH/Vde).
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const cwdNote = "Note: your current working directory is"

// Divergences: no unicode NFC normalization (the builtin NFC-normalizes;
// stdlib-only here), an unresolvable home directory leaves "~" literal
// instead of throwing, and the literal strings "undefined" and "null"
// resolve to root (models emit them for "no path"; the builtin instead
// begged the model not to in the schema description).
func resolveAgainst(p, root string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", errors.New("Path contains null bytes")
	}
	p = strings.TrimSpace(p)
	if p == "" || p == "undefined" || p == "null" {
		return root, nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if p == "~" {
				return home, nil
			}
			return filepath.Join(home, p[2:]), nil
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Join(root, p), nil
}

// resolveSymlinks is EvalSymlinks that falls back to the input unchanged
// when resolution fails (nonexistent path, permission error).
func resolveSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// rebasePath maps p from resolved space back to argv space: when p sits
// under resolved, its prefix is swapped for orig. Everything the tool
// reports stays in the form the caller supplied (see execute).
func rebasePath(p, resolved, orig string) string {
	if resolved == orig || !strings.HasPrefix(p, resolved) {
		return p
	}
	rest := p[len(resolved):]
	if rest != "" && rest[0] != filepath.Separator {
		return p // prefix match mid-component, not a child path
	}
	return orig + rest
}

// Non-path inputs (a "--" separator line, a bare line number) fail
// filepath.Rel and pass through unchanged, which matches what Node
// path.relative hands back for them.
func relativizePath(abs, root string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

func didYouMean(resolved, root string) string {
	sep := string(filepath.Separator)
	parent := filepath.Dir(root)
	n := resolved
	if rp, err := filepath.EvalSymlinks(filepath.Dir(resolved)); err == nil {
		n = filepath.Join(rp, filepath.Base(resolved))
	}
	prefix := parent
	if parent != sep {
		prefix = parent + sep
	}
	if !strings.HasPrefix(n, prefix) || strings.HasPrefix(n, root+sep) || n == root {
		return ""
	}
	rel, err := filepath.Rel(parent, n)
	if err != nil {
		return ""
	}
	candidate := filepath.Join(root, rel)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
