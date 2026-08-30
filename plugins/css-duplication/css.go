package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"sort"
	"strings"
)

// A stylesheet's real defect is almost never a syntax error -- it is the same
// declaration block written again under a new selector because the cascade
// went unused. This file finds exactly that: rules that share a byte-identical
// (normalized) body within the SAME at-rule context.

// Rule is one declaration block: where it is, what selects it, and its
// normalized declarations.
type Rule struct {
	Selector string
	Line     int
	Context  string   // the enclosing at-rule prelude chain, "" at top level
	Decls    []string // normalized, source order
}

// Group is a set of rules that declare identical things in the same context.
type Group struct {
	Context string
	Decls   []string // sorted, the shared body
	Rules   []Rule
}

// declBlockAtRules hold declarations rather than nested rules, so their bodies
// are parsed like an ordinary rule. Anything else starting with '@' is a
// container whose body is scanned for nested rules.
var declBlockAtRules = map[string]bool{
	"@font-face":           true,
	"@page":                true,
	"@property":            true,
	"@counter-style":       true,
	"@font-palette-values": true,
	"@viewport":            true,
}

// stripComments removes /* ... */ while preserving string literals and byte
// offsets are irrelevant afterwards -- line numbers are, so newlines inside a
// comment are kept.
func stripComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			for i += 2; i < len(src); i++ {
				if src[i] == '\n' {
					b.WriteByte('\n') // keep line numbers honest
				}
				if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
					i++
					break
				}
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// normalizeDecl collapses whitespace and lowercases the property name, so
// `COLOR:   var(--accent)` and `color: var(--accent)` are one declaration.
// The value keeps its case (font names, url()s and custom-property values are
// case-sensitive).
func normalizeDecl(decl string) string {
	decl = strings.Join(strings.Fields(decl), " ")
	if decl == "" {
		return ""
	}
	i := strings.Index(decl, ":")
	if i < 0 {
		return decl
	}
	prop := strings.TrimSpace(decl[:i])
	// A custom property's NAME is case-sensitive; a standard property's is not.
	if !strings.HasPrefix(prop, "--") {
		prop = strings.ToLower(prop)
	}
	return prop + ": " + strings.TrimSpace(decl[i+1:])
}

// splitDecls splits a declaration block on top-level semicolons -- not the
// ones inside url(data:...;base64,...) or a quoted string.
func splitDecls(body string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	depth := 0
	flush := func() {
		if d := normalizeDecl(cur.String()); d != "" {
			out = append(out, d)
		}
		cur.Reset()
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if quote != 0 {
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(body) {
				i++
				cur.WriteByte(body[i])
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			cur.WriteByte(c)
		case '(':
			depth++
			cur.WriteByte(c)
		case ')':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(c)
		case ';':
			if depth == 0 {
				flush()
				continue
			}
			cur.WriteByte(c)
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// atRuleName returns the lowercased at-rule keyword of a prelude ("@media
// (min-width: 40em)" -> "@media"), or "" when the prelude is a selector.
func atRuleName(prelude string) string {
	if !strings.HasPrefix(prelude, "@") {
		return ""
	}
	name := prelude
	if i := strings.IndexAny(prelude, " \t\n("); i > 0 {
		name = prelude[:i]
	}
	return strings.ToLower(name)
}

// ParseRules walks a stylesheet and returns every declaration block with its
// selector, line and enclosing at-rule context. @keyframes is skipped whole:
// identical from/to bodies are normal there, not a defect.
func ParseRules(src string) []Rule {
	src = stripComments(src)
	var rules []Rule
	var walk func(s string, lineBase int, context string)
	walk = func(s string, lineBase int, context string) {
		line := lineBase
		start := 0
		var quote byte
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c == '\n' {
				line++
			}
			if quote != 0 {
				if c == '\\' && i+1 < len(s) {
					i++
					continue
				}
				if c == quote {
					quote = 0
				}
				continue
			}
			switch c {
			case '\'', '"':
				quote = c
			case ';': // an at-statement (@import, @layer a, b;) -- no block
				start = i + 1
			case '{':
				raw := s[start:i]
				prelude := strings.TrimSpace(raw)
				// The line the SELECTOR starts on, not the line the previous
				// rule ended on: blank lines between rules sit in `raw` too, so
				// only the newlines inside the trimmed selector count back.
				trimmed := strings.TrimLeft(raw, " \t\r\n")
				preludeLine := line - strings.Count(trimmed, "\n")
				body, end := readBlock(s, i)
				bodyLine := line
				at := atRuleName(prelude)
				switch {
				case at == "@keyframes" || at == "@-webkit-keyframes":
					// skipped entirely
				case at != "" && !declBlockAtRules[at]:
					nested := context
					if nested != "" {
						nested += " "
					}
					walk(body, bodyLine, nested+strings.Join(strings.Fields(prelude), " "))
				default:
					if decls := splitDecls(body); len(decls) > 0 {
						rules = append(rules, Rule{
							Selector: strings.Join(strings.Fields(prelude), " "),
							Line:     preludeLine,
							Context:  context,
							Decls:    decls,
						})
					}
				}
				line += strings.Count(s[i:end], "\n")
				i = end
				start = i + 1
			}
		}
	}
	walk(src, 1, "")
	return rules
}

// readBlock returns the body between the '{' at open and its matching '}',
// plus the index of that '}' (or len(s)-1 when unbalanced -- a truncated file
// must never panic a hook).
func readBlock(s string, open int) (string, int) {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[open+1 : i], i
			}
		}
	}
	return s[open+1:], len(s) - 1
}

// FindDuplicates groups rules that share an identical body in the same
// context. Thresholds keep it quiet: a multi-declaration body is a finding at
// two copies, a single-declaration body only at three -- one repeated
// `display: none` is noise, three copies of one block is a base rule waiting
// to be written.
func FindDuplicates(rules []Rule) []Group {
	type key struct{ context, body string }
	order := []key{}
	byKey := map[key][]Rule{}
	for _, r := range rules {
		sorted := append([]string(nil), r.Decls...)
		sort.Strings(sorted)
		k := key{r.Context, strings.Join(sorted, "; ")}
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], r)
	}

	var groups []Group
	for _, k := range order {
		rs := byKey[k]
		if len(rs) < 2 {
			continue
		}
		if len(rs[0].Decls) < 2 && len(rs) < 3 {
			continue
		}
		// Identical selectors are a different bug (a rule written twice), and
		// one the model rarely commits; dedupe so a repeated selector in two
		// files' worth of copy-paste does not read as N distinct rules.
		seen := set.New[string]()
		uniq := rs[:0:0]
		for _, r := range rs {
			if seen.Contains(r.Selector) {
				continue
			}
			seen.Add(r.Selector)
			uniq = append(uniq, r)
		}
		if len(uniq) < 2 {
			continue
		}
		sorted := append([]string(nil), uniq[0].Decls...)
		sort.Strings(sorted)
		groups = append(groups, Group{Context: k.context, Decls: sorted, Rules: uniq})
	}

	// Strongest first, because the report is capped: a 4-copy, 2-declaration
	// block is the base rule someone forgot to write, while three copies of
	// `margin: 0` on different elements may be nothing. Sorting by copies x
	// declarations keeps the finding that matters from falling off the end
	// (measured: on the stylesheet that motivated this plugin, the real
	// 3-selector link block was pushed out of an unsorted top 5 by
	// `border-bottom: 0`).
	sort.SliceStable(groups, func(i, j int) bool {
		wi := len(groups[i].Rules) * len(groups[i].Decls)
		wj := len(groups[j].Rules) * len(groups[j].Decls)
		if wi != wj {
			return wi > wj
		}
		return groups[i].Rules[0].Line < groups[j].Rules[0].Line
	})
	return groups
}
