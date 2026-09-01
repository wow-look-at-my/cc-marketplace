package main

import (
	"os"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// A word carries its literal text plus whether that text is the whole story.
// `sed -i s/a/b/ $TARGET` parses fine but its operand is unknowable, and a write
// aimed at an unknowable path is exactly the ambiguity that must deny.
type word struct {
	text   string
	static bool
}

type redirTarget struct {
	op   syntax.RedirOperator
	file word
}

// One executable unit: an argv with the directory it runs in, plus the redirects
// attached to it.
type segment struct {
	argv   []word
	cwd    string
	redirs []redirTarget
	// relocated marks a GIT_DIR / GIT_WORK_TREE prefix. Those move the repository
	// away from the directory the path words describe, so where the write lands
	// stops being knowable from the text.
	relocated bool
	// stdinScript marks a stage fed its input by a pipe or a heredoc. An
	// interpreter in that position is running a script nothing can resolve.
	stdinScript bool
}

var repoRelocatingEnv = set.Of[string]("GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE")

const (
	maxWalkDepth   = 32
	maxSegments    = 4096
	maxScriptBytes = 1 << 20
	maxScriptDepth = 3
	unknownDirText = ""
)

// parseSegments flattens a command into every unit that will execute, in
// execution order, tracking the working directory across the sequence. It
// reports the blockers it hit -- a script it could not analyse -- separately
// from the segments, because those deny on their own.
func parseSegments(command, cwd string) (segs []segment, blockers []string, ok bool) {
	f, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, nil, false
	}
	w := &walker{}
	base := cwd
	w.stmts(f.Stmts, &base)
	return w.segs, w.blockers, true
}

type walker struct {
	segs        []segment
	blockers    []string
	depth       int
	scriptDepth int
	piped       bool
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
	stdin := w.piped
	for _, r := range st.Redirs {
		if r == nil || r.Word == nil {
			continue
		}
		w.scanSubst(r.Word, *cwd)
		if r.Op == syntax.Hdoc || r.Op == syntax.DashHdoc || r.Op == syntax.WordHdoc {
			stdin = true
			continue // a heredoc is input; its "target" is the delimiter word
		}
		rs = append(rs, redirTarget{op: r.Op, file: wordText(r.Word)})
	}
	w.command(st.Cmd, cwd, rs, stdin)
}

// command dispatches on node type. The cwd pointer is shared only where the
// shell itself shares one: `&&`, `||`, `;` and a brace block run in the current
// shell, so a cd carries forward. A pipe stage, a subshell and a conditional
// body each get a copy. A background `&` changes nothing here -- the writes it
// performs are the same writes, so its segments are walked like any other.
func (w *walker) command(c syntax.Command, cwd *string, rs []redirTarget, stdin bool) {
	if c == nil || w.full() {
		return
	}
	w.depth++
	defer func() { w.depth-- }()
	if w.depth > maxWalkDepth {
		w.blockers = append(w.blockers, "the command nests deeper than this hook will follow")
		return
	}

	switch x := c.(type) {
	case *syntax.CallExpr:
		w.call(x, cwd, rs, stdin)
	case *syntax.BinaryCmd:
		if x.Op == syntax.Pipe || x.Op == syntax.PipeAll {
			left, right := *cwd, *cwd
			was := w.piped
			w.piped = stdin
			w.stmt(x.X, &left)
			w.piped = true
			w.stmt(x.Y, &right)
			w.piped = was
			return
		}
		w.stmt(x.X, cwd)
		w.stmt(x.Y, cwd)
	case *syntax.Subshell:
		w.bare(rs, *cwd)
		w.isolated(x.Stmts, *cwd)
	case *syntax.Block:
		w.bare(rs, *cwd)
		w.stmts(x.Stmts, cwd)
	case *syntax.IfClause:
		w.bare(rs, *cwd)
		w.isolated(x.Cond, *cwd)
		w.isolated(x.Then, *cwd)
		if x.Else != nil {
			w.command(x.Else, cwd, nil, false)
		}
	case *syntax.WhileClause:
		w.bare(rs, *cwd)
		w.isolated(x.Cond, *cwd)
		w.isolated(x.Do, *cwd)
	case *syntax.ForClause:
		w.bare(rs, *cwd)
		w.isolated(x.Do, *cwd)
	case *syntax.CaseClause:
		w.bare(rs, *cwd)
		for _, item := range x.Items {
			if item != nil {
				w.isolated(item.Stmts, *cwd)
			}
		}
	case *syntax.FuncDecl:
		// A function body executes when the function is called, and this hook
		// cannot know whether that happens in this command or a later one. Its
		// writes are walked either way.
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
	default:
		w.bare(rs, *cwd)
	}
}

// bare records a redirect that hangs off a compound rather than an argv:
// `{ ...; } > f` and `(...) > f` truncate f with no command word of their own.
func (w *walker) bare(rs []redirTarget, cwd string) {
	if len(rs) > 0 {
		w.segs = append(w.segs, segment{cwd: cwd, redirs: rs})
	}
}

func (w *walker) isolated(sts []*syntax.Stmt, cwd string) {
	local := cwd
	w.stmts(sts, &local)
}

func (w *walker) call(c *syntax.CallExpr, cwd *string, rs []redirTarget, stdin bool) {
	relocated := false
	for _, a := range c.Assigns {
		if a == nil {
			continue
		}
		if a.Name != nil && repoRelocatingEnv.Contains(a.Name.Value) {
			relocated = true
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
		w.bare(rs, *cwd)
		return
	}
	// An `env VAR=VAL` prefix carries the same relocation as a bare one.
	for _, a := range argv {
		if i := strings.Index(a.text, "="); i > 0 && repoRelocatingEnv.Contains(a.text[:i]) {
			relocated = true
		}
	}
	eff := stripWrappers(argv)
	if len(eff) == 0 {
		w.bare(rs, *cwd)
		return
	}
	name := commandName(eff[0].text)
	if name == "cd" || name == "pushd" {
		*cwd = resolveDir(*cwd, eff)
		return
	}
	if w.expand(name, eff, *cwd) {
		w.bare(rs, *cwd)
		return
	}
	w.segs = append(w.segs, segment{
		argv: eff, cwd: *cwd, redirs: rs,
		relocated: relocated, stdinScript: stdin,
	})
}

// expand follows the indirections whose text this hook can still read: an alias
// or a `sh -c` string is shell source, and a shell script file is a file of it.
// Following them is what stops `bash ./write.sh` from being a one-word bypass of
// every rule below. It reports true when the call was consumed by the walk.
func (w *walker) expand(name string, eff []word, cwd string) bool {
	switch {
	case name == "alias":
		// `alias w='sed -i'` hides a writer behind a name the walk would
		// otherwise read as an unknown program.
		for _, a := range eff[1:] {
			if i := strings.Index(a.text, "="); i > 0 {
				w.script(a.text[i+1:], cwd, "an alias definition")
			}
		}
		return true
	case isShell(name):
		return w.shellCall(eff, cwd)
	case name == "source" || name == ".":
		if len(eff) > 1 {
			w.scriptFile(eff[1], cwd)
		}
		return true
	case name == "find":
		w.findExec(eff, cwd)
		return false
	}
	// `./deploy.sh` names a file rather than a program on PATH. When its shebang
	// says shell, its text is readable and gets the same treatment -- except for
	// an installed Claude Code plugin script, which this walk treats as an
	// opaque trusted program instead. Those scripts are pre-vetted, versioned,
	// harness-invoked infrastructure, not content the model authored, and one of
	// them can legitimately contain a debug-log write behind a guard this
	// dataflow-free walk cannot see through (an env-var-gated `>>"$path"`), which
	// would otherwise deny on an ambiguous target every time the script runs.
	if strings.Contains(eff[0].text, "/") && eff[0].static {
		p := abs(cwd, eff[0].text)
		if isInstalledPluginScript(p) {
			return false
		}
		if hasShellShebang(p) {
			w.scriptFile(eff[0], cwd)
			return true
		}
	}
	return false
}

func (w *walker) shellCall(eff []word, cwd string) bool {
	for i := 1; i < len(eff); i++ {
		t := eff[i].text
		if t == "-c" {
			if i+1 >= len(eff) {
				return true
			}
			if !eff[i+1].static {
				w.blockers = append(w.blockers, "a shell -c script assembled from an expansion, whose writes cannot be resolved")
				return true
			}
			w.script(eff[i+1].text, cwd, "a shell -c script")
			return true
		}
		if strings.HasPrefix(t, "-") {
			continue
		}
		w.scriptFile(eff[i], cwd)
		return true
	}
	// A bare `bash` reads its script from stdin, which is not in the text.
	w.blockers = append(w.blockers, "a shell reading its script from stdin, whose writes cannot be resolved")
	return true
}

// script parses shell source found inside the command and folds its segments
// into the same walk, so a write two levels down is judged like a write at top
// level.
func (w *walker) script(src, cwd, what string) {
	if w.scriptDepth >= maxScriptDepth {
		w.blockers = append(w.blockers, what+", nested deeper than this hook will follow")
		return
	}
	f, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	if err != nil {
		w.blockers = append(w.blockers, what+", which does not parse as shell")
		return
	}
	w.scriptDepth++
	w.isolated(f.Stmts, cwd)
	w.scriptDepth--
}

// scriptFile follows a shell script on disk. A script that does not exist writes
// nothing, so it is left alone; one that exists and cannot be read or parsed is
// the write-elsewhere-then-run bypass and denies.
func (w *walker) scriptFile(f word, cwd string) {
	if !f.static {
		w.blockers = append(w.blockers, "a script path built from an expansion, whose writes cannot be resolved")
		return
	}
	path := abs(cwd, f.text)
	if path == "" {
		w.blockers = append(w.blockers, "a script at a path that is not statically known")
		return
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return
	}
	if st.Size() > maxScriptBytes {
		w.blockers = append(w.blockers, "the script "+f.text+", which is too large to analyse")
		return
	}
	src, err := os.ReadFile(path)
	if err != nil {
		w.blockers = append(w.blockers, "the script "+f.text+", which cannot be read")
		return
	}
	w.script(string(src), cwd, "the script "+f.text)
}

// findExec lifts the utility out of `find ... -exec <argv> ;` and walks it as a
// call of its own. Without this, `find . -name '*.go' -exec sed -i s/a/b/ {} +`
// reads as one invocation of a program called find.
func (w *walker) findExec(eff []word, cwd string) {
	for i := 1; i < len(eff); i++ {
		t := eff[i].text
		if t != "-exec" && t != "-execdir" && t != "-ok" && t != "-okdir" {
			continue
		}
		var sub []word
		for j := i + 1; j < len(eff); j++ {
			if eff[j].text == ";" || eff[j].text == "+" {
				break
			}
			sub = append(sub, eff[j])
		}
		if len(sub) == 0 {
			continue
		}
		// -execdir runs in the directory of each match, which the text does not
		// name, so the paths in it resolve against a directory nothing knows.
		dir := cwd
		if t == "-execdir" || t == "-okdir" {
			dir = unknownDirText
		}
		w.emit(sub, dir)
	}
}

// emit runs a lifted argv through the same wrapper stripping and expansion the
// walker applies to a call it parsed itself.
func (w *walker) emit(argv []word, cwd string) {
	eff := stripWrappers(argv)
	if len(eff) == 0 {
		return
	}
	if w.expand(commandName(eff[0].text), eff, cwd) {
		return
	}
	w.segs = append(w.segs, segment{argv: eff, cwd: cwd})
}

// A command substitution runs its own shell, so its cd is contained, but the
// command inside is every bit as able to write as one at top level.
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

// The word, wrapper and path vocabulary this walk is built on lives in
// shellwords.go.
