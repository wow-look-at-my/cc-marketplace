package main

import (
	"path"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// The allow path's reader. It gives up on anything it cannot fully resolve --
// a redirect, an expansion, a substitution -- because refusing to read is the
// safe answer when the verdict would be "allow". The deny path in
// deny_process.go walks the same tree and never gives up, for the same reason
// in reverse.

func parseAllCommands(command string) [][]string {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return nil
	}

	// Reject any command with output redirections (>, >>, etc.)
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

	// Check for dangerous constructs
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
		// Handle &&, ||, |
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

func extractWord(word *syntax.Word) string {
	var parts []string
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			parts = append(parts, p.Value)
		case *syntax.SglQuoted:
			parts = append(parts, p.Value)
		case *syntax.DblQuoted:
			// Double quotes are OK if they only contain literals
			for _, qpart := range p.Parts {
				if lit, ok := qpart.(*syntax.Lit); ok {
					parts = append(parts, lit.Value)
				} else {
					return "" // Contains variable expansion or similar
				}
			}
		default:
			return "" // Unknown part type
		}
	}
	return strings.Join(parts, "")
}

// extractExecSubCommands extracts sub-commands from exec-style flags.
// e.g., for args ["-name", "*.h", "-exec", "grep", "-l", "pattern", "{}", ";"]
// with execFlags ["-exec"], returns [["grep", "-l", "pattern"]].
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
		// Collect args until ";" or "+"
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

// isAllowedRedirect reports whether a redirect is safe to auto-allow: a merge
// of stderr into stdout, or a write to /dev/null or under /tmp/.
func isAllowedRedirect(r *syntax.Redirect) bool {
	fd := "1"
	if r.N != nil {
		fd = r.N.Value
	}
	target := redirectTarget(r)

	// stderr merged into stdout
	if r.Op == syntax.DplOut {
		return fd == "2" && target == "1"
	}

	switch r.Op {
	case syntax.RdrOut, syntax.AppOut:
		if fd != "1" && fd != "2" {
			return false
		}
	case syntax.RdrAll, syntax.AppAll:
		// &> and &>> carry stdout and stderr together
	default:
		return false
	}

	return isSafeRedirectPath(target)
}

// isSafeRedirectPath reports whether target is /dev/null or under /tmp/.
// path.Clean defeats a traversal such as /tmp/../etc/passwd.
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
