package main

import (
	"path"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// Process-level deny: a rule matches the process a statement would START, not
// the argv spelling, because a spelling list can never be finished.
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

// Commands that run their leading non-flag, non-assignment argument as a new
// process. Stripping them resolves `env python3` and `sudo -E python3`.
var execWrappers = set.Of(
	"env", "sudo", "doas", "nohup", "command",
	"exec", "nice", "ionice", "stdbuf", "setsid",
	"time", "timeout", "xargs", "watch", "script",
	"uv", "uvx", "pipx", "poetry", "hatch",
	"pdm", "conda", "rye", "micromamba", "npx", "bunx",
)

// Wrapper subcommands preceding the real command: `uv run python`.
var wrapperSubcommands = set.Of("run", "exec")

// Arguments meaning "the script arrives on stdin".
var stdinMarkers = set.Of("-", "/dev/stdin")

// resolveArgs splits words into the process name and its own arguments, after
// stripping assignments, wrappers and wrapper flags. Empty name = nothing runs.
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

// matchesDenied reports whether a resolved name is the denied program. A
// trailing version is the same program, so `python` covers every suffixed spelling.
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
// rather than a file. fedByStdin carries what the argument list cannot show.
func isInlineScript(d ProcessRule, args []string, fedByStdin bool) bool {
	namesAScript := false
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			namesAScript = true
		}
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
	// A named script makes stdin its input, not the program.
	return fedByStdin && !namesAScript
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
// never gives up: an unresolvable part is skipped, so `"py"thon3` still yields
// something to inspect instead of dropping the command from consideration.
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
