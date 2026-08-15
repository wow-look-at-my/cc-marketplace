package main

import (
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// A word carries its literal text plus whether that text is the whole story.
// `rm $TARGET` parses fine but its operand is unknowable, and a destructive
// verb pointed at an unknowable path is exactly the ambiguity that must deny.
type word struct {
	text   string
	static bool
}

type redirTarget struct {
	op   syntax.RedirOperator
	file word
}

// One executable unit: an argv with the directory it runs in, or a bare
// redirect (`(...) > f` truncates f with no argv of its own).
type segment struct {
	argv   []word
	cwd    string
	redirs []redirTarget
	// gitEnv marks a GIT_DIR / GIT_WORK_TREE prefix. Those relocate the repo
	// away from the directory the path words describe, so the segment's target
	// stops being knowable from the text.
	gitEnv bool
}

// Env vars that move a git command's idea of where the repository is.
var repoRelocatingEnv = map[string]bool{
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_COMMON_DIR": true,
}

const (
	maxWalkDepth   = 32
	maxSegments    = 4096
	unknownDirText = ""
)

// parseSegments flattens a command into every unit that will execute, in
// execution order, tracking the working directory across the sequence. It
// reports false when the text is not parseable at all -- the caller decides
// what to do with that, and for a destructive verb the answer is deny.
func parseSegments(command, cwd string) ([]segment, bool) {
	f, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, false
	}
	w := &walker{}
	base := cwd
	w.stmts(f.Stmts, &base)
	return w.segs, true
}

type walker struct {
	segs  []segment
	depth int
}

func (w *walker) full() bool { return len(w.segs) >= maxSegments }

func (w *walker) stmts(sts []*syntax.Stmt, cwd *string) {
	for _, st := range sts {
		w.stmt(st, cwd)
	}
}

func (w *walker) stmt(st *syntax.Stmt, cwd *string) {
	if st == nil || w.full() {
		return
	}
	var rs []redirTarget
	for _, r := range st.Redirs {
		if r == nil || r.Word == nil {
			continue
		}
		w.scanSubst(r.Word, *cwd)
		rs = append(rs, redirTarget{op: r.Op, file: wordText(r.Word)})
	}
	if len(rs) > 0 {
		w.segs = append(w.segs, segment{cwd: *cwd, redirs: rs})
	}
	w.command(st.Cmd, cwd)
}

// command dispatches on node type. The cwd pointer is shared only where the
// shell itself shares one: `&&`, `||`, `;` and a brace block run in the current
// shell, so a cd carries forward. A pipe stage, a subshell and a conditional
// body each get a copy.
func (w *walker) command(c syntax.Command, cwd *string) {
	if c == nil || w.full() {
		return
	}
	w.depth++
	defer func() { w.depth-- }()
	if w.depth > maxWalkDepth {
		return
	}

	switch x := c.(type) {
	case *syntax.CallExpr:
		w.call(x, cwd)
	case *syntax.BinaryCmd:
		if x.Op == syntax.Pipe || x.Op == syntax.PipeAll {
			left, right := *cwd, *cwd
			w.stmt(x.X, &left)
			w.stmt(x.Y, &right)
			return
		}
		w.stmt(x.X, cwd)
		w.stmt(x.Y, cwd)
	case *syntax.Subshell:
		w.isolated(x.Stmts, *cwd)
	case *syntax.Block:
		w.stmts(x.Stmts, cwd)
	case *syntax.IfClause:
		w.isolated(x.Cond, *cwd)
		w.isolated(x.Then, *cwd)
		if x.Else != nil {
			w.command(x.Else, cwd)
		}
	case *syntax.WhileClause:
		w.isolated(x.Cond, *cwd)
		w.isolated(x.Do, *cwd)
	case *syntax.ForClause:
		w.isolated(x.Do, *cwd)
	case *syntax.CaseClause:
		for _, item := range x.Items {
			if item != nil {
				w.isolated(item.Stmts, *cwd)
			}
		}
	case *syntax.FuncDecl:
		if x.Body != nil {
			local := *cwd
			w.stmt(x.Body, &local)
		}
	case *syntax.TimeClause:
		if x.Stmt != nil {
			w.stmt(x.Stmt, cwd)
		}
	case *syntax.CoprocClause:
		if x.Stmt != nil {
			local := *cwd
			w.stmt(x.Stmt, &local)
		}
	}
}

func (w *walker) isolated(sts []*syntax.Stmt, cwd string) {
	local := cwd
	w.stmts(sts, &local)
}

func (w *walker) call(c *syntax.CallExpr, cwd *string) {
	gitEnv := false
	for _, a := range c.Assigns {
		if a == nil {
			continue
		}
		if a.Name != nil && repoRelocatingEnv[a.Name.Value] {
			gitEnv = true
		}
		if a.Value != nil {
			w.scanSubst(a.Value, *cwd)
		}
	}
	argv := make([]word, 0, len(c.Args))
	for _, wd := range c.Args {
		w.scanSubst(wd, *cwd)
		argv = append(argv, wordText(wd))
	}
	if len(argv) == 0 {
		return
	}
	// An `env VAR=VAL` prefix carries the same relocation as a bare one.
	for _, a := range argv {
		if i := strings.Index(a.text, "="); i > 0 && repoRelocatingEnv[a.text[:i]] {
			gitEnv = true
		}
	}
	eff := stripWrappers(argv)
	if len(eff) == 0 {
		return
	}
	if name := commandName(eff[0].text); name == "cd" || name == "pushd" {
		*cwd = resolveDir(*cwd, eff)
		return
	}
	w.segs = append(w.segs, segment{argv: eff, cwd: *cwd, gitEnv: gitEnv})
}

// A command substitution runs its own shell, so its cd is contained, but the
// command inside is every bit as able to delete work as one at top level.
func (w *walker) scanSubst(wd *syntax.Word, cwd string) {
	if wd == nil || w.full() || w.depth > maxWalkDepth {
		return
	}
	for _, p := range wd.Parts {
		switch x := p.(type) {
		case *syntax.CmdSubst:
			w.depth++
			w.isolated(x.Stmts, cwd)
			w.depth--
		case *syntax.ProcSubst:
			w.depth++
			w.isolated(x.Stmts, cwd)
			w.depth--
		case *syntax.DblQuoted:
			for _, dp := range x.Parts {
				if sub, ok := dp.(*syntax.CmdSubst); ok {
					w.depth++
					w.isolated(sub.Stmts, cwd)
					w.depth--
				}
			}
		}
	}
}

func wordText(wd *syntax.Word) word {
	if wd == nil {
		return word{static: true}
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
	return word{text: b.String(), static: static}
}

// commandName resolves the spelling to the program: `\git`, `/usr/bin/git` and
// `"git"` are all git.
func commandName(t string) string {
	t = strings.TrimPrefix(t, `\`)
	if t == "" {
		return ""
	}
	return filepath.Base(t)
}

// stripWrappers peels the layers that stand between the words as written and
// the program that actually runs. Enumerating spellings of a program can never
// be finished, so this resolves instead: one rule for git covers `sudo -E env
// GIT_DIR=x git`, `xargs git` and `timeout 5 git` alike.
func stripWrappers(argv []word) []word {
	for len(argv) > 0 {
		switch commandName(argv[0].text) {
		case "env":
			argv = argv[1:]
			for len(argv) > 0 {
				t := argv[0].text
				if t == "-u" || t == "--unset" || t == "-C" {
					argv = dropN(argv, 2)
					continue
				}
				if strings.HasPrefix(t, "-") {
					argv = argv[1:]
					continue
				}
				// A VAR=VAL prefix, not the program.
				if i := strings.Index(t, "="); i > 0 {
					argv = argv[1:]
					continue
				}
				break
			}
		case "sudo", "doas":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				t := argv[0].text
				if t == "-u" || t == "-g" || t == "-p" || t == "-C" || t == "--user" || t == "--group" {
					argv = dropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
			for len(argv) > 0 && strings.Contains(argv[0].text, "=") && !strings.HasPrefix(argv[0].text, "-") {
				argv = argv[1:]
			}
		case "command", "builtin", "exec", "nohup", "nice", "setsid", "stdbuf", "ionice", "time":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				argv = argv[1:]
			}
		case "timeout":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				t := argv[0].text
				if t == "-s" || t == "-k" || t == "--signal" || t == "--kill-after" {
					argv = dropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
			argv = dropN(argv, 1) // the duration operand
		case "xargs":
			argv = argv[1:]
			for len(argv) > 0 && strings.HasPrefix(argv[0].text, "-") {
				if xargsValueFlags[argv[0].text] {
					argv = dropN(argv, 2)
					continue
				}
				argv = argv[1:]
			}
		default:
			return argv
		}
	}
	return argv
}

// xargs' value-taking flags, so the utility word is located rather than
// mistaken for a flag argument.
var xargsValueFlags = map[string]bool{
	"-n": true, "-I": true, "-i": true, "-L": true, "-P": true, "-s": true,
	"-d": true, "-E": true, "-a": true,
	"--max-args": true, "--replace": true, "--max-lines": true,
	"--max-procs": true, "--max-chars": true, "--delimiter": true,
	"--eof": true, "--arg-file": true,
}

func dropN(argv []word, n int) []word {
	if len(argv) <= n {
		return nil
	}
	return argv[n:]
}

// resolveDir computes the directory a cd lands in. An operand that is not
// statically known -- `cd $DIR`, `cd -` -- yields the unknown marker, which
// makes every later destructive segment in the chain undecidable and therefore
// denied.
func resolveDir(cwd string, eff []word) string {
	for _, a := range eff[1:] {
		if a.text == "-" {
			return unknownDirText
		}
		if strings.HasPrefix(a.text, "-") {
			continue
		}
		if !a.static {
			return unknownDirText
		}
		if filepath.IsAbs(a.text) {
			return filepath.Clean(a.text)
		}
		if cwd == unknownDirText {
			return unknownDirText
		}
		return filepath.Clean(filepath.Join(cwd, a.text))
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Clean(home) // bare `cd`
	}
	return unknownDirText
}
