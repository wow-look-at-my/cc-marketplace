// Package shellwalk holds the shell-reading vocabulary that no-work-loss and
// enhanced-auto-allow both need: what a word says, which program a spelling
// actually names, and whether an invocation names a script of its own.
//
// The two plugins keep their own segmentation and their own verdicts -- one
// fails closed, the other fails open, deliberately. What they must NOT keep
// separately is the answer to "which program does this run", because a wrapper
// or a spelling either plugin misreads is a rule the other still enforces.
package shellwalk

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Word is one argv element: the text it expands to, and whether that text is
// the whole story. Static is false when a part could expand to anything -- a
// parameter, a substitution, a glob. A caller granting permission must treat a
// non-static word as unknown; a caller refusing may still read Text.
type Word struct {
	Text   string
	Static bool
}

// WordText renders one syntax word. An unresolvable part contributes nothing to
// Text and clears Static, so `"py"thon3` still yields "python3" to inspect
// while `$X` yields the empty string and says so.
func WordText(wd *syntax.Word) Word {
	if wd == nil {
		return Word{Static: true}
	}
	var b strings.Builder
	static := true
	for _, p := range wd.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, dp := range x.Parts {
				if lit, ok := dp.(*syntax.Lit); ok {
					b.WriteString(lit.Value)
					continue
				}
				static = false
			}
		default:
			static = false
		}
	}
	return Word{Text: b.String(), Static: static}
}

// Words renders a whole argument list.
func Words(args []*syntax.Word) []Word {
	out := make([]Word, 0, len(args))
	for _, a := range args {
		out = append(out, WordText(a))
	}
	return out
}

// CommandName resolves a spelling to the program it names: `\sed`,
// `/usr/bin/sed` and `"sed"` are all sed.
func CommandName(t string) string {
	t = strings.TrimPrefix(t, `\`)
	if t == "" {
		return ""
	}
	return filepath.Base(t)
}

// MatchesProgram reports whether a resolved name is the named program. A
// trailing version is the same program, so "python" covers python3 and
// python3.11 without listing either.
func MatchesProgram(name, want string) bool {
	if name == want {
		return true
	}
	if !strings.HasPrefix(name, want) {
		return false
	}
	for _, r := range name[len(want):] {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}
