// paths.go carries the path resolution/relativization helpers and the
// did-you-mean suggester, copied from the sibling glob plugin (they port
// the same claude-code internals: Vq/QZH/Vde).
package main

import (
	"os"
	"path/filepath"
	"strings"
)

const cwdNote = "Note: your current working directory is"

// resolveAgainst resolves p against root (absolute paths pass through
// cleaned), like Node path.resolve(root, p). Divergence: no unicode NFC
// normalization (the builtin NFC-normalizes; stdlib-only here).
func resolveAgainst(p, root string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(root, p)
}

// relativizePath mirrors QZH (2.1.116:cli.js:35616-35619): root-relative
// when under root, absolute otherwise (including the faithful quirk that
// any relative form starting with ".." — even a "..foo" sibling name —
// falls back to absolute). Non-path inputs (a "--" separator line, a
// bare line number) fail filepath.Rel and pass through unchanged, which
// matches what Node path.relative hands back for them.
func relativizePath(abs, root string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

// didYouMean ports Vde (2.1.207:cli.js:44437-44455): when the missing
// path resolved into the parent of root but outside root, re-root it
// under root and suggest that absolute path if it exists.
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
