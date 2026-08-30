package main

import (
	"path"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// ProcessRule matches a program by what it resolves to, not how it is spelled.
type ProcessRule struct {
	Name string
	// Behavior is the section this rule came from: allow, ask or deny.
	Behavior string
	Message  string
	// InlineOnly denies a script given on the argv or stdin, not `node x.js`.
	InlineOnly bool
	// EvalFlags take a script (-e); a short one matches in a cluster (-pe).
	EvalFlags []string
	// EvalSubcommands are subcommand words meaning the same thing (deno eval).
	EvalSubcommands []string
}

// Wrappers that run their leading non-flag, non-assignment argument as a new
// process. Stripping them is what makes `env python3` and `sudo -E python3`
// resolve to python.
var execWrappers = set.Of[string](
	"env", "sudo", "doas", "nohup", "command",
	"exec", "nice", "ionice", "stdbuf", "setsid",
	"time", "timeout", "xargs", "watch", "script",
	"uv", "uvx", "pipx", "poetry", "hatch",
	"pdm", "conda", "rye", "micromamba", "npx", "bunx",
)

// Wrapper subcommands preceding the real command: `uv run python`.
var wrapperSubcommands = set.Of[string]("run", "exec")

// Arguments meaning "the script arrives on stdin".
var stdinMarkers = set.Of[string]("-", "/dev/stdin")

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
		if execWrappers.Contains(base) || wrapperSubcommands.Contains(base) {
			continue
		}
		return base, args[i+1:]
	}
	return "", nil
}

// matchesDenied reports whether a resolved process name is the denied program.
// A trailing version counts as the same program, so a `python` entry covers
// every versioned spelling of it without enumerating them.
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
func isInlineScript(d ProcessRule, args []string, fedByStdin bool) bool {
	if fedByStdin {
		return true
	}
	for i, arg := range args {
		if stdinMarkers.Contains(arg) {
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

// matchProcessRule walks every statement in the parse tree -- command
// substitutions, subshells, loops and conditionals included, all of which the
// allow path refuses to read -- and returns the earliest rule that matches.
//
// The whole tree is walked because bailing to passthrough is right when
// GRANTING permission and wrong when REFUSING it: a denied program would
// otherwise slip through wrapped in a construct the whitelist declines to
// parse. Matching is structural rather than by spelling, because a spelling
// list can never be finished -- `python3`, `env python3`, `sudo -E python` and
// `uv run python` are the same process start, and the next spelling is always
// unenumerated.
//
// Known limit: a program named only inside a string handed to another
// interpreter (`bash -c "python3 x.py"`) is not visible here, because at parse
// time that is an opaque argument, not a command.
func matchProcessRule(command string, denies []ProcessRule) (string, string) {
	if len(denies) == 0 {
		return "", ""
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return "", ""
	}

	// `echo 'code' | node` smuggles a script past any argument check.
	piped := set.New[*syntax.Stmt]()
	syntax.Walk(file, func(n syntax.Node) bool {
		if b, ok := n.(*syntax.BinaryCmd); ok && (b.Op == syntax.Pipe || b.Op == syntax.PipeAll) {
			piped.Add(b.Y)
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
		fedByStdin := piped.Contains(stmt)
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
