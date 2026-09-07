package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// varTable holds the shell variables this hook has proven can only ever hold
// one value at the point they are read: assigned exactly once, from a fully
// static right-hand side, outside every construct that could run it more
// than once or not at all -- a loop, a conditional, a subshell, a pipeline,
// a background job, a function body. Anything less certain never enters
// this map, and a name absent from it resolves exactly as it always has --
// the word carrying it denies rather than guesses.
type varTable map[string]word

// varMutatingCommands names every command that can set a variable this scan
// does not read as an assignment. Seeing any of them anywhere in the
// command disables resolution entirely, because such a command can rewrite
// a name this scan has no way to notice.
var varMutatingCommands = set.Of[string](
	"read", "mapfile", "readarray", "getopts", "unset",
	"eval", "set", "declare", "local", "typeset", "export",
)

// mutatesAVariable reports whether a call can bind a name behind this scan's
// back. Everything in varMutatingCommands does, with one exception worth
// spelling out: `set` sets SHELL OPTIONS as well as positional parameters,
// and `set -e` or `set +o pipefail` binds nothing at all. Treating those as a
// hazard disabled resolution for every script that opens with `set -e`, which
// is most of them -- one such line refused a whole test suite over a path the
// script built from its own variable. Only an operand form rebinds anything.
func mutatesAVariable(eff []word) bool {
	name := commandName(eff[0].text)
	if !varMutatingCommands.Contains(name) {
		return false
	}
	if name != "set" {
		return true
	}
	return setRebindsParameters(eff[1:])
}

// setRebindsParameters reports whether a `set` call carries operands rather
// than options. A bare `set`, a `--`, and any word that is not an option all
// count; `-o`/`+o` consume the option name that follows them.
func setRebindsParameters(args []word) bool {
	if len(args) == 0 {
		return true
	}
	optionName := false
	for _, a := range args {
		if optionName {
			optionName = false
			continue
		}
		if !a.static {
			return true
		}
		if !strings.HasPrefix(a.text, "-") && !strings.HasPrefix(a.text, "+") {
			return true
		}
		if a.text == "--" || a.text == "-" || a.text == "+" {
			return true
		}
		if a.text == "-o" || a.text == "+o" {
			optionName = true
		}
	}
	return false
}

// unsafeVarNames finds every variable name this hook must never resolve:
// assigned more than once anywhere, assigned inside a construct that can
// run zero times or more than once, or bound by a for-loop. abort reports a
// varMutatingCommands hit anywhere in the tree, which disables resolution
// for the whole command regardless of what unsafe names.
// multiply names every variable assigned more than once, which is a weaker
// condition than unsafe: a name assigned once inside an `if` holds one value
// or none, never two. That is enough for a resolution claiming only the
// DIRECTORY a value sits in, and not enough for one claiming the value.
func unsafeVarNames(stmts []*syntax.Stmt) (unsafe, multiply map[string]bool, abort bool) {
	c := &varScan{counts: map[string]int{}, unsafe: map[string]bool{}}
	c.stmts(stmts, false)
	multiply = map[string]bool{}
	for name, n := range c.counts {
		if n > 1 {
			multiply[name] = true
		}
	}
	return c.unsafe, multiply, c.abort
}

type varScan struct {
	counts map[string]int
	unsafe map[string]bool
	abort  bool
}

// record notes one assignment to name. A second assignment anywhere, or a
// first one made where it might run zero or more than once, means the name
// can no longer be trusted to hold one value.
func (c *varScan) record(name string, risky bool) {
	c.counts[name]++
	if risky || c.counts[name] > 1 {
		c.unsafe[name] = true
	}
}

func (c *varScan) stmts(sts []*syntax.Stmt, risky bool) {
	for _, st := range sts {
		c.stmt(st, risky)
	}
}

func (c *varScan) stmt(st *syntax.Stmt, risky bool) {
	if st == nil || c.abort {
		return
	}
	if st.Background {
		risky = true
	}
	for _, r := range st.Redirs {
		if r != nil {
			c.scanWord(r.Word)
		}
	}
	c.command(st.Cmd, risky)
}

// command mirrors the dispatch in segment.go's walker.command: the same
// node types, the same idea of which branch can run more than once or might
// not run at all. It answers a narrower question, so it tracks risk rather
// than cwd or destructive verbs.
func (c *varScan) command(cmd syntax.Command, risky bool) {
	if cmd == nil || c.abort {
		return
	}
	switch x := cmd.(type) {
	case *syntax.CallExpr:
		for _, a := range x.Assigns {
			if a == nil || a.Name == nil {
				continue
			}
			c.record(a.Name.Value, risky)
			if a.Value != nil {
				c.scanWord(a.Value)
			}
		}
		for _, a := range x.Args {
			c.scanWord(a)
		}
		if len(x.Args) > 0 {
			argv := make([]word, 0, len(x.Args))
			for _, wd := range x.Args {
				argv = append(argv, wordText(wd))
			}
			if eff := stripWrappers(argv); len(eff) > 0 && mutatesAVariable(eff) {
				c.abort = true
			}
		}
	case *syntax.BinaryCmd:
		if x.Op == syntax.Pipe || x.Op == syntax.PipeAll {
			c.stmt(x.X, true)
			c.stmt(x.Y, true)
			return
		}
		c.stmt(x.X, risky)
		c.stmt(x.Y, risky)
	case *syntax.Subshell:
		c.stmts(x.Stmts, true)
	case *syntax.Block:
		c.stmts(x.Stmts, risky)
	case *syntax.IfClause:
		c.stmts(x.Cond, true)
		c.stmts(x.Then, true)
		if x.Else != nil {
			c.command(x.Else, true)
		}
	case *syntax.WhileClause:
		c.stmts(x.Cond, true)
		c.stmts(x.Do, true)
	case *syntax.ForClause:
		if wi, ok := x.Loop.(*syntax.WordIter); ok {
			if wi.Name != nil {
				// The loop variable is bound to a new value on every
				// iteration, so no single value survives past the loop.
				c.record(wi.Name.Value, true)
			}
			for _, it := range wi.Items {
				c.scanWord(it)
			}
		}
		c.stmts(x.Do, true)
	case *syntax.CaseClause:
		for _, item := range x.Items {
			if item != nil {
				c.stmts(item.Stmts, true)
			}
		}
	case *syntax.FuncDecl:
		if x.Body != nil {
			c.stmt(x.Body, true)
		}
	case *syntax.TimeClause:
		if x.Stmt != nil {
			c.stmt(x.Stmt, risky)
		}
	case *syntax.CoprocClause:
		if x.Stmt != nil {
			c.stmt(x.Stmt, true)
		}
	}
}

// scanWord looks inside a word for command substitutions and process
// substitutions. Each forks its own subshell and can assign anything, so
// its content is always risky regardless of where the word itself sits.
func (c *varScan) scanWord(wd *syntax.Word) {
	if wd == nil || c.abort {
		return
	}
	for _, p := range wd.Parts {
		c.scanPart(p)
	}
}

func (c *varScan) scanPart(p syntax.WordPart) {
	switch x := p.(type) {
	case *syntax.CmdSubst:
		c.stmts(x.Stmts, true)
	case *syntax.ProcSubst:
		c.stmts(x.Stmts, true)
	case *syntax.DblQuoted:
		for _, dp := range x.Parts {
			c.scanPart(dp)
		}
	case *syntax.ParamExp:
		if x.Exp != nil && x.Exp.Word != nil {
			c.scanWord(x.Exp.Word)
		}
	}
}

// tempRoots names the directories a temp file is created under. A path under
// one of them is outside every guarded root by construction.
var tempRoots = []string{"/tmp", "/var/tmp"}

// mktempPath resolves `$(mktemp ...)` to a path in the temp directory. The
// exact basename is chosen by mktemp at run time and nothing here can know
// it, but the DIRECTORY is what decides whether a write lands in the working
// tree, and mktemp with no template or a temp-rooted one cannot leave the
// temp directory. A template naming any other directory resolves to nothing,
// because `mktemp ./buildXXXX` really does write beside the source.
func mktempPath(wd *syntax.Word) (word, bool) {
	sub, ok := soleCmdSubst(wd)
	if !ok || len(sub.Stmts) != 1 || sub.Stmts[0] == nil {
		return word{}, false
	}
	call, ok := sub.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 || len(call.Assigns) > 0 {
		return word{}, false
	}
	argv := make([]word, 0, len(call.Args))
	for _, a := range call.Args {
		argv = append(argv, wordText(a))
	}
	if eff := stripWrappers(argv); len(eff) == 0 || commandName(eff[0].text) != "mktemp" {
		return word{}, false
	}
	for i, a := range call.Args {
		if i == 0 || strings.HasPrefix(argv[i].text, "-") {
			continue
		}
		if !underTempRoot(a) {
			return word{}, false
		}
	}
	return word{text: filepath.Join(os.TempDir(), "mktemp-created-file"), static: true}, true
}

// underTempRoot reports whether a template names a path inside the temp
// directory. It reads the word's own leading text rather than the resolved
// one, because the common spelling is `"${TMPDIR:-/tmp}/fooXXXX"` and that
// resolves to nothing. os.TempDir reads the same variable with the same
// default, so the hook and the command agree on where it points.
func underTempRoot(wd *syntax.Word) bool {
	if wd == nil || len(wd.Parts) == 0 {
		return false
	}
	parts := wd.Parts
	if q, ok := parts[0].(*syntax.DblQuoted); ok && len(q.Parts) > 0 {
		parts = q.Parts
	}
	var head string
	switch x := parts[0].(type) {
	case *syntax.Lit:
		head = x.Value
	case *syntax.SglQuoted:
		head = x.Value
	case *syntax.ParamExp:
		// `${TMPDIR:-/tmp}` is the ordinary spelling, and its default has to
		// be temp-rooted too or the fallback leaves the temp directory.
		if x.Param == nil || x.Param.Value != "TMPDIR" ||
			(x.Exp != nil && !underTempRoot(x.Exp.Word)) {
			return false
		}
		// A following part must start the next path segment; `${TMPDIR}x`
		// names a sibling of the temp directory, not something inside it.
		if len(parts) > 1 {
			lit, ok := parts[1].(*syntax.Lit)
			if !ok || !strings.HasPrefix(lit.Value, "/") {
				return false
			}
		}
		return true
	default:
		return false
	}
	for _, r := range tempRoots {
		if head == r || strings.HasPrefix(head, r+"/") {
			return true
		}
	}
	return false
}

// soleCmdSubst unwraps a word whose entire content is one command
// substitution, quoted or not.
func soleCmdSubst(wd *syntax.Word) (*syntax.CmdSubst, bool) {
	if wd == nil || len(wd.Parts) != 1 {
		return nil, false
	}
	part := wd.Parts[0]
	if q, ok := part.(*syntax.DblQuoted); ok {
		if len(q.Parts) != 1 {
			return nil, false
		}
		part = q.Parts[0]
	}
	sub, ok := part.(*syntax.CmdSubst)
	return sub, ok
}

// resolveWord renders a word the same way wordText does, except a plain
// $NAME or ${NAME} reference is replaced with vars[NAME] when present.
// Anything vars does not cover -- an absent name, an operator, an index, a
// slice -- resolves exactly as it always has: unresolved, with no text.
func resolveWord(wd *syntax.Word, vars varTable) word {
	if wd == nil {
		return word{static: true}
	}
	var b strings.Builder
	static := true
	for _, p := range wd.Parts {
		if !resolvePart(p, vars, &b) {
			static = false
		}
	}
	return word{text: b.String(), static: static}
}

// resolvePart appends one part's literal text into b, reporting whether the
// text is fully known. It never writes partial text for a part it cannot
// resolve, matching shellwalk.WordText: an unresolved word carries no text
// that could pass for a real path.
func resolvePart(p syntax.WordPart, vars varTable, b *strings.Builder) bool {
	switch x := p.(type) {
	case *syntax.Lit:
		b.WriteString(x.Value)
		return true
	case *syntax.SglQuoted:
		b.WriteString(x.Value)
		return true
	case *syntax.DblQuoted:
		ok := true
		for _, dp := range x.Parts {
			if !resolvePart(dp, vars, b) {
				ok = false
			}
		}
		return ok
	case *syntax.ParamExp:
		if v, found := simpleVar(x, vars); found {
			b.WriteString(v.text)
			return true
		}
		return false
	default:
		return false
	}
}

// simpleVar resolves a plain $NAME / ${NAME} reference -- no negation,
// length, width, index, slice, replace, name-listing, or default/pattern
// operator. Any of those means the value depends on something this scan
// does not evaluate, so it is treated exactly like an unresolved variable.
func simpleVar(x *syntax.ParamExp, vars varTable) (word, bool) {
	if x == nil || x.Param == nil {
		return word{}, false
	}
	// ${BASH_SOURCE[0]} names the file the running script was read from, which
	// is the one path the walk already knows when it follows a script. Element
	// 0 is the only element that means that, so any other index is refused
	// with every other operator below.
	if x.Param.Value == "BASH_SOURCE" && isIndexZero(x.Index) &&
		!x.Excl && !x.Length && !x.Width && x.Slice == nil && x.Repl == nil && x.Names == 0 && x.Exp == nil {
		v, ok := vars["0"]
		return v, ok
	}
	if x.Excl || x.Length || x.Width || x.Index != nil || x.Slice != nil ||
		x.Repl != nil || x.Names != 0 || x.Exp != nil {
		return word{}, false
	}
	v, ok := vars[x.Param.Value]
	return v, ok
}

// isIndexZero reports whether an array index is the literal 0.
func isIndexZero(idx syntax.ArithmExpr) bool {
	w, ok := idx.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	return ok && lit.Value == "0"
}
