package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// Extracting comments is where a checker earns or loses its accuracy. A string
// literal holding "// not a comment" must not be checked, and a comment must be
// paired with the code it annotates -- "is this comment about the thing it sits
// on" is one of the questions being asked.
//
// Go files go through the real parser, which gets both right by construction.
// Everything else is line-based with a string-literal skipper: good enough to
// find the prose, and honest about being a heuristic.

const codeContextLines = 6

// blocksIn returns the comment blocks of a file, keeping only those with at
// least one line the session touched.
func blocksIn(path string, src []byte, touched map[int]bool) []Block {
	var all []Block
	if filepath.Ext(path) == ".go" {
		all = goBlocks(path, src)
	} else {
		all = lineBlocks(path, src)
	}

	lines := strings.Split(string(src), "\n")
	var kept []Block
	for _, b := range all {
		if !touchedAny(b, touched) {
			continue
		}
		b.Code = codeAfter(lines, b.Line+len(b.Raw)-1)
		kept = append(kept, b)
	}
	return kept
}

func touchedAny(b Block, touched map[int]bool) bool {
	for i := 0; i < len(b.Raw); i++ {
		if touched[b.Line+i] {
			return true
		}
	}
	return false
}

// codeAfter returns the first few non-blank, non-comment lines below a block --
// the declaration the comment is attached to.
func codeAfter(lines []string, lastCommentLine int) string {
	var out []string
	for i := lastCommentLine; i < len(lines) && len(out) < codeContextLines; i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" || isCommentLine(l) {
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

func isCommentLine(trimmed string) bool {
	for _, p := range []string{"//", "#", "/*", "*", "*/", "--"} {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

// goBlocks uses go/parser, so string literals are never mistaken for comments
// and a doc comment is known to belong to its declaration.
func goBlocks(path string, src []byte) []Block {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		// A file mid-edit does not parse. Fall back rather than skip: a
		// checker that goes quiet exactly when the code is in flux is worth
		// very little.
		return lineBlocks(path, src)
	}
	var out []Block
	for _, g := range f.Comments {
		pos := fset.Position(g.Pos())
		var raw []string
		for _, c := range g.List {
			raw = append(raw, c.Text)
		}
		text := strings.TrimSpace(g.Text())
		if text == "" {
			continue
		}
		out = append(out, Block{
			File: path,
			Line: pos.Line,
			Text: text,
			Raw:  raw,
		})
	}
	// Directives are instructions to a tool, not claims about the code.
	return filterOut(out, isDirective)
}

// lineBlocks is the generic reader: consecutive line comments coalesce into one
// block, and /* */ spans are taken whole. It skips string literals so a URL or
// a regex containing "//" does not become a comment.
func lineBlocks(path string, src []byte) []Block {
	lines := strings.Split(string(src), "\n")
	var out []Block
	var cur []string
	var curLine int
	inSpan := false

	flush := func() {
		if len(cur) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(stripMarkers(cur), "\n"))
		if text != "" {
			out = append(out, Block{File: path, Line: curLine, Text: text, Raw: append([]string(nil), cur...)})
		}
		cur = nil
	}

	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		switch {
		case inSpan:
			cur = append(cur, l)
			if strings.Contains(l, "*/") {
				inSpan = false
				flush()
			}
		case strings.HasPrefix(trimmed, "/*"):
			if len(cur) > 0 {
				flush()
			}
			curLine = i + 1
			cur = []string{l}
			if !strings.Contains(trimmed[2:], "*/") {
				inSpan = true
			} else {
				flush()
			}
		case commentStart(l) >= 0:
			if len(cur) == 0 {
				curLine = i + 1
			}
			cur = append(cur, l[commentStart(l):])
		default:
			flush()
		}
	}
	flush()
	return filterOut(out, isDirective)
}

// commentStart returns the index where a line comment begins, or -1. It walks
// the line tracking quote state so a marker inside a string is not a comment.
func commentStart(line string) int {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '/':
			if i+1 < len(line) && line[i+1] == '/' {
				return i
			}
		case '#':
			return i
		case '-':
			if i+1 < len(line) && line[i+1] == '-' {
				return i
			}
		}
	}
	return -1
}

func stripMarkers(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		l = strings.TrimSpace(l)
		for _, p := range []string{"/**", "/*", "*/", "//", "#", "--"} {
			l = strings.TrimPrefix(l, p)
		}
		l = strings.TrimSuffix(l, "*/")
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "*"))
		out = append(out, l)
	}
	return out
}

// isDirective spots machine-read comments -- build tags, lint pragmas, codegen
// markers. They are instructions, not claims, and have no truth to check.
func isDirective(b Block) bool {
	first := strings.TrimSpace(strings.SplitN(b.Text, "\n", 2)[0])
	for _, p := range []string{
		"go:", "+build", "nolint", "lint:", "eslint", "prettier", "ts-", "@ts-",
		"noinspection", "codegen:", "Code generated", "shellcheck", "!",
	} {
		if strings.HasPrefix(first, p) {
			return true
		}
	}
	return false
}

func filterOut(blocks []Block, drop func(Block) bool) []Block {
	var out []Block
	for _, b := range blocks {
		if !drop(b) {
			out = append(out, b)
		}
	}
	return out
}

var _ = ast.Print
