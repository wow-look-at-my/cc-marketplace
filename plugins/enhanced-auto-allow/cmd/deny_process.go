package main

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Process-level deny.
//
// The allow path answers "is this command on the whitelist", and it deliberately
// bails to passthrough on anything it cannot fully read -- an output redirect, a
// command substitution, a subshell. That is the right posture for GRANTING
// permission and exactly the wrong one for REFUSING it: a denied program would
// slip through by being wrapped in any construct the allow path declines to
// parse. So this walks the whole tree instead and asks one question of every
// statement: what process would this start, and how is it being fed?
//
// It answers structurally rather than by pattern, because a pattern list can
// never be finished. `python3`, `/usr/bin/python3`, `env python3`, `python3.11`,
// `sudo -E python`, `uv run python` are one process start wearing six spellings,
// and the next one is always the spelling nobody enumerated.
type DenyProcess struct {
	Name    string
	Message string
	// InlineOnly denies just the forms that execute a script handed to the
	// interpreter on the command line or on stdin, leaving `node script.js`
	// alone. Interpreters that must stay usable (node runs this environment's
	// own hooks) are denied this way; ones with no business running at all are
	// denied outright.
	InlineOnly bool
	// EvalFlags are the flags that take a script as their value (-e, --eval).
	// For single-dash flags they also match inside a cluster, so perl's -pe,
	// -ne and -lane are caught without listing every combination.
	EvalFlags []string
	// EvalSubcommands are argv[1] forms meaning the same thing (deno eval).
	EvalSubcommands []string
}

// Wrappers that run their first non-flag, non-assignment argument as a new
// process. Stripping them is what makes `env python3` and `sudo -E python3`
// resolve to python.
var execWrappers = map[string]bool{
	"env": true, "sudo": true, "doas": true, "nohup": true, "command": true,
	"exec": true, "nice": true, "ionice": true, "stdbuf": true, "setsid": true,
	"time": true, "timeout": true, "xargs": true, "watch": true, "script": true,
	"uv": true, "uvx": true, "pipx": true, "poetry": true, "hatch": true,
	"pdm": true, "conda": true, "rye": true, "micromamba": true, "npx": true, "bunx": true,
}

// Subcommands of a wrapper that precede the real command: `uv run python`,
// `poetry run python`, `conda run -n env python`.
var wrapperSubcommands = map[string]bool{"run": true, "exec": true}

// Arguments meaning "the script arrives on stdin".
var stdinMarkers = map[string]bool{"-": true, "/dev/stdin": true}

// resolveArgs splits a command's words into the process name and the arguments
// belonging to it, after stripping VAR=VAL assignments, wrapper commands and the
// flags those wrappers consumed. Returns ("", nil) when nothing is left to run.
func resolveArgs(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if eq := strings.Index(arg, "="); eq > 0 && !strings.HasPrefix(arg, "-") && !strings.Contains(arg[:eq], "/") {
			continue // VAR=VAL assignment
		}
		if strings.HasPrefix(arg, "-") {
			continue // a wrapper's own flag
		}
		base := path.Base(arg)
		if execWrappers[base] || wrapperSubcommands[base] {
			continue
		}
		return base, args[i+1:]
	}
	return "", nil
}

// matchesDenied reports whether a resolved process name is the denied program.
// A trailing version counts as the same program, so one `python` entry covers
// python3, python3.11 and python2.7 without enumerating them.
func matchesDenied(name, denied string) bool {
	if name == denied {
		return true
	}
	if !strings.HasPrefix(name, denied) {
		return false
	}
	for _, r := range name[len(denied):] {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

// isInlineScript reports whether an invocation hands the interpreter a script
// rather than pointing it at a file: an eval flag, an eval subcommand, a stdin
// marker, or a heredoc/pipe feeding it. fedByStdin carries the cases the
// argument list alone cannot show.
func isInlineScript(d DenyProcess, args []string, fedByStdin bool) bool {
	if fedByStdin {
		return true
	}
	for i, arg := range args {
		if stdinMarkers[arg] {
			return true
		}
		for _, f := range d.EvalFlags {
			if arg == f {
				return true
			}
			// A single-dash cluster: perl -pe, -ne, -lane, ruby -ne.
			if len(f) == 2 && f[0] == '-' && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") &&
				strings.ContainsRune(arg[1:], rune(f[1])) {
				return true
			}
		}
		if i == 0 {
			for _, sub := range d.EvalSubcommands {
				if arg == sub {
					return true
				}
			}
		}
	}
	return false
}

// deniedProcess walks every statement in the parse tree -- including those inside
// command substitutions, subshells, loops and conditionals, which the allow path
// refuses to read -- and returns the first denied process it finds.
//
// Known limit: a program named only inside a string handed to another
// interpreter (`bash -c "python3 x.py"`) is not visible here, because at parse
// time that is an opaque argument, not a command.
func deniedProcess(command string, denies []DenyProcess) (string, string) {
	if len(denies) == 0 {
		return "", ""
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return "", ""
	}

	// A statement on the receiving end of a pipe reads its input from it, which
	// is how `echo 'code' | node` smuggles a script past an argument check.
	piped := map[*syntax.Stmt]bool{}
	syntax.Walk(file, func(n syntax.Node) bool {
		if b, ok := n.(*syntax.BinaryCmd); ok && (b.Op == syntax.Pipe || b.Op == syntax.PipeAll) {
			piped[b.Y] = true
		}
		return true
	})

	var hitName, hitMsg string
	syntax.Walk(file, func(n syntax.Node) bool {
		if hitName != "" {
			return false
		}
		stmt, ok := n.(*syntax.Stmt)
		if !ok {
			return true
		}
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok {
			return true
		}
		var words []string
		for _, w := range call.Args {
			words = append(words, wordLiteral(w))
		}
		name, args := resolveArgs(words)
		if name == "" {
			return true
		}
		fedByStdin := piped[stmt]
		for _, r := range stmt.Redirs {
			switch r.Op {
			case syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc, syntax.RdrIn:
				fedByStdin = true
			}
		}
		for _, d := range denies {
			if !matchesDenied(name, d.Name) {
				continue
			}
			if d.InlineOnly && !isInlineScript(d, args, fedByStdin) {
				continue
			}
			hitName, hitMsg = name, d.Message
			return false
		}
		return true
	})
	return hitName, hitMsg
}

// wordLiteral renders a word for name resolution only. Unlike extractWord it
// never gives up on a word it cannot fully resolve: unresolvable parts are
// skipped, so `"py"thon3` still yields something to inspect rather than dropping
// the whole command from consideration.
func wordLiteral(w *syntax.Word) string {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				if lit, ok := dp.(*syntax.Lit); ok {
					b.WriteString(lit.Value)
				}
			}
		}
	}
	return b.String()
}
