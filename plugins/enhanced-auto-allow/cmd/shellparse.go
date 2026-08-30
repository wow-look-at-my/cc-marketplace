package main

import (
	"path"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// The allow path's reading of a command. It is deliberately timid: anything it
// cannot resolve to a plain argv -- an expansion, a substitution, a redirect it
// does not vouch for -- returns nil and the command falls through to the normal
// permission flow. Giving up is the right answer when GRANTING permission. The
// deny path in deny_process.go takes the opposite posture on the same tree.

func parseAllCommands(command string) [][]string {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return nil
	}

	if hasOutputRedirect(file) {
		return nil
	}

	var allCommands [][]string
	for _, stmt := range file.Stmts {
		commands := extractCommands(stmt.Cmd)
		if commands == nil {
			return nil
		}
		allCommands = append(allCommands, commands...)
	}

	return allCommands
}

func extractCommands(cmd syntax.Command) [][]string {
	if cmd == nil {
		return nil
	}

	if hasDangerousConstruct(cmd) {
		return nil
	}

	switch c := cmd.(type) {
	case *syntax.CallExpr:
		args := extractCallArgs(c)
		if args == nil {
			return nil
		}
		return [][]string{args}

	case *syntax.BinaryCmd:
		// &&, || and |
		left := extractCommands(c.X.Cmd)
		if left == nil {
			return nil
		}
		right := extractCommands(c.Y.Cmd)
		if right == nil {
			return nil
		}
		return append(left, right...)

	default:
		return nil
	}
}

func extractCallArgs(call *syntax.CallExpr) []string {
	var args []string
	for _, word := range call.Args {
		arg := extractWord(word)
		if arg == "" {
			return nil
		}
		args = append(args, arg)
	}
	return args
}

// extractWord flattens a word to its literal text, or returns "" when any part
// of it is an expansion the allow path must not guess the value of.
func extractWord(word *syntax.Word) string {
	var parts []string
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			parts = append(parts, p.Value)
		case *syntax.SglQuoted:
			parts = append(parts, p.Value)
		case *syntax.DblQuoted:
			for _, qpart := range p.Parts {
				lit, ok := qpart.(*syntax.Lit)
				if !ok {
					return ""
				}
				parts = append(parts, lit.Value)
			}
		default:
			return ""
		}
	}
	return strings.Join(parts, "")
}

// extractExecSubCommands pulls the sub-commands out of exec-style flags, so
// `find . -exec grep -l pattern {} ;` is evaluated as the grep it runs.
func extractExecSubCommands(args []string, execFlags []string) [][]string {
	flagSet := set.New[string]()
	for _, f := range execFlags {
		flagSet.Add(f)
	}

	var result [][]string
	for i := 0; i < len(args); i++ {
		if !flagSet.Contains(args[i]) {
			continue
		}
		// The sub-command runs until ";" or "+" terminates it.
		var subCmd []string
		i++
		for i < len(args) {
			a := args[i]
			if a == ";" || a == "+" {
				break
			}
			if a != "{}" {
				subCmd = append(subCmd, a)
			}
			i++
		}
		if len(subCmd) > 0 {
			result = append(result, subCmd)
		}
	}
	return result
}

func hasOutputRedirect(node syntax.Node) bool {
	found := false
	syntax.Walk(node, func(n syntax.Node) bool {
		if stmt, ok := n.(*syntax.Stmt); ok {
			for _, r := range stmt.Redirs {
				switch r.Op {
				case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll,
					syntax.DplOut, syntax.ClbOut, syntax.RdrInOut:
					if isAllowedRedirect(r) {
						continue
					}
					found = true
					return false
				}
			}
		}
		return !found
	})
	return found
}

// isAllowedRedirect reports whether a redirect operation is safe to auto-allow.
// Permitted: duplicating stderr onto stdout, and stdout or stderr writes to
// /dev/null or under /tmp/.
func isAllowedRedirect(r *syntax.Redirect) bool {
	fd := "1"
	if r.N != nil {
		fd = r.N.Value
	}
	target := redirectTarget(r)

	if r.Op == syntax.DplOut {
		return fd == "2" && target == "1"
	}

	switch r.Op {
	case syntax.RdrOut, syntax.AppOut:
		if fd != "1" && fd != "2" {
			return false
		}
	case syntax.RdrAll, syntax.AppAll:
		// &> and &>> redirect both stdout and stderr
	default:
		return false
	}

	return isSafeRedirectPath(target)
}

// isSafeRedirectPath accepts /dev/null or a path under /tmp/, cleaned so that a
// traversal like /tmp/../etc/passwd does not pass.
func isSafeRedirectPath(target string) bool {
	if target == "/dev/null" {
		return true
	}
	return strings.HasPrefix(path.Clean(target), "/tmp/")
}

func redirectTarget(r *syntax.Redirect) string {
	if r.Word == nil {
		return ""
	}
	var parts []string
	for _, p := range r.Word.Parts {
		lit, ok := p.(*syntax.Lit)
		if !ok {
			return ""
		}
		parts = append(parts, lit.Value)
	}
	return strings.Join(parts, "")
}

// hasDangerousConstruct reports a construct whose value is not knowable from the
// text, so the allow path must not vouch for the command containing it.
func hasDangerousConstruct(node syntax.Node) bool {
	dangerous := false
	syntax.Walk(node, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.CmdSubst, *syntax.ParamExp, *syntax.ArithmExp, *syntax.ProcSubst:
			dangerous = true
			return false
		}
		return true
	})
	return dangerous
}
