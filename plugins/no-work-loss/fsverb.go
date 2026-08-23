package main

import (
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// classifyFS covers the destruction that never mentions git: removing a file,
// moving another one over it, or truncating it with a redirect. Each is scoped
// to the paths it names -- a repo being dirty elsewhere is no reason to refuse
// `rm` on a build artifact.
func classifyFS(seg segment) []*finding {
	var out []*finding

	for _, r := range seg.redirs {
		if !truncating(r) {
			continue
		}
		out = append(out, &finding{
			label: "> " + r.file.text, haz: hazTracked | hazUntracked, dir: seg.cwd,
			paths:   []word{r.file},
			rewrite: ">> " + r.file.text + "   # append, or commit the file first",
		})
	}

	if len(seg.argv) == 0 {
		return out
	}
	flags, operands := splitPlain(seg.argv[1:])

	switch commandName(seg.argv[0].text) {
	case "rm":
		if len(operands) == 0 {
			return out
		}
		out = append(out, &finding{
			label: "rm", haz: hazTracked | hazUntracked, dir: seg.cwd,
			paths:   operands,
			rewrite: "git stash push -u -- " + shellJoin(operands) + "   # or commit them first",
		})

	case "mv":
		// Only the destination loses content; the source's bytes survive the
		// move. With several sources the destination is a directory, so each
		// lands under it by basename.
		if len(operands) < 2 {
			return out
		}
		dst := operands[len(operands)-1]
		srcs := operands[:len(operands)-1]
		targets := []word{dst}
		if len(srcs) > 1 || isDir(seg.cwd, dst) {
			targets = nil
			for _, s := range srcs {
				targets = append(targets, word{
					text:   filepath.Join(dst.text, filepath.Base(s.text)),
					static: dst.static && s.static,
				})
			}
		}
		out = append(out, &finding{
			label: "mv", haz: hazTracked | hazUntracked, dir: seg.cwd,
			paths:   targets,
			rewrite: "git stash push -u -- " + shellJoin(targets) + " && " + shellJoin(seg.argv),
		})

	case "tee":
		// tee truncates every file it is given unless appending. Worth
		// covering on its own: the sibling cleanup-bash-cmds plugin rewrites a
		// trailing `> file` into `| tee file`, so this is a shape that arrives
		// without anyone typing it.
		if flags["-a"] || flags["--append"] || len(operands) == 0 {
			return out
		}
		out = append(out, &finding{
			label: "tee", haz: hazTracked | hazUntracked, dir: seg.cwd,
			paths:   operands,
			rewrite: "tee -a " + shellJoin(operands) + "   # append instead of truncating",
		})

	case "truncate":
		if !zeroSize(seg.argv) || len(operands) == 0 {
			return out
		}
		out = append(out, &finding{
			label: "truncate -s 0", haz: hazTracked | hazUntracked, dir: seg.cwd,
			paths:   operands,
			rewrite: "git stash push -u -- " + shellJoin(operands),
		})
	}
	return out
}

func truncating(r redirTarget) bool {
	switch r.op {
	case syntax.RdrOut, syntax.ClbOut, syntax.RdrAll:
		return true
	case syntax.DplOut:
		// `2>&1` duplicates a descriptor; `>&file` truncates a file.
		t := r.file.text
		if t == "-" {
			return false
		}
		for _, c := range t {
			if c < '0' || c > '9' {
				return true
			}
		}
		return false
	}
	return false
}

// splitPlain is the non-git flag split: same short-flag bundling, no
// subcommand notion.
func splitPlain(rest []word) (map[string]bool, []word) {
	flags := map[string]bool{}
	var operands []word
	dashDash := false
	for _, a := range rest {
		t := a.text
		switch {
		case dashDash:
			operands = append(operands, a)
		case t == "--":
			dashDash = true
		case strings.HasPrefix(t, "--"):
			name := t
			if i := strings.Index(t, "="); i > 0 {
				name = t[:i]
			}
			flags[name] = true
		case len(t) > 1 && strings.HasPrefix(t, "-"):
			flags[t] = true
			for _, c := range t[1:] {
				flags["-"+string(c)] = true
			}
		default:
			operands = append(operands, a)
		}
	}
	return flags, operands
}

// zeroSize reports a truncate that empties the file, in either spelling:
// `-s 0`, `-s0` or `--size=0`.
func zeroSize(argv []word) bool {
	for i, a := range argv {
		t := a.text
		if t == "-s" || t == "--size" {
			return i+1 < len(argv) && isZero(argv[i+1].text)
		}
		if v, ok := strings.CutPrefix(t, "--size="); ok {
			return isZero(v)
		}
		if v, ok := strings.CutPrefix(t, "-s"); ok && v != "" {
			return isZero(v)
		}
	}
	return false
}

func isZero(s string) bool { return s == "0" }

func isDir(cwd string, w word) bool {
	if !w.static || cwd == unknownDirText {
		return false
	}
	p := w.text
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
