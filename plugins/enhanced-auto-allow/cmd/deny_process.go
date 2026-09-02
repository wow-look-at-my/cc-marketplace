package main

import (
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
	"shellwalk"
)

// Process-level deny: a rule matches the process a statement would START, not
// the argv spelling, because a spelling list can never be finished.
//
// Reading the words, peeling the wrappers, and separating a named script from a
// script arriving on stdin all live in shellwalk, shared with no-work-loss: a
// wrapper that either plugin misreads is a rule the other still enforces.
type ProcessRule struct {
	Name string
	// The section this rule came from: allow, ask or deny.
	Behavior string
	Message  string
	// Deny a script handed to the interpreter, sparing `node script.js`.
	InlineOnly bool
	// Flags taking a script as their value; a single-dash flag matches inside a cluster too, covering perl's -pe and -lane.
	EvalFlags []string
	// Subcommand spellings of the same thing (deno eval).
	EvalSubcommands []string
}

// isInlineScript reports whether an invocation hands the interpreter a script
// rather than a file. fedByStdin carries what the argument list cannot show.
func isInlineScript(d ProcessRule, args []shellwalk.Word, fedByStdin bool) bool {
	for i, a := range args {
		arg := a.Text
		if shellwalk.StdinMarkers.Contains(arg) {
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
	// A named script makes stdin its input, not the program.
	return fedByStdin && !shellwalk.NamesAScript(args)
}

// matchProcessRule walks EVERY statement, including the substitutions,
// subshells and conditionals the allow path refuses to read -- a denied program
// must never be a `$(...)` away from running. Known gap: a program named inside
// a string handed to another interpreter is an argument here, not a command.
func matchProcessRule(command string, denies []ProcessRule) (string, string) {
	if len(denies) == 0 {
		return "", ""
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return "", ""
	}

	// `echo 'code' | node` smuggles a script past an argument check.
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
		name, args := shellwalk.ResolveProgram(shellwalk.Words(call.Args))
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
			if !shellwalk.MatchesProgram(name, d.Name) {
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
