package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The stylesheet this plugin exists because of: no `a` rule, the same block
// under five selectors, and a session about to write a sixth.
const dashboardCSS = `
:root { --accent: #2f81f7; }
#setup-section a:not(.btn) { color: var(--accent); text-decoration: none; }
#setup-section a:not(.btn):hover { text-decoration: underline; }
.crumbs a { color: var(--accent); text-decoration: none; }
.crumbs a:hover { text-decoration: underline; }
a.gh-slug { color: var(--accent); text-decoration: none; font: inherit; }
a.gh-slug:hover { text-decoration: underline; }
a.kv-jump { color: var(--accent); text-decoration: none; }
a.kv-jump:hover { text-decoration: underline; }
.site-footer #server-version a { color: var(--accent); text-decoration: none; }
`

func groupWith(t *testing.T, groups []Group, decl string) Group {
	t.Helper()
	for _, g := range groups {
		if strings.Contains(strings.Join(g.Decls, "; "), decl) {
			return g
		}
	}
	t.Fatalf("no group containing %q in %#v", decl, groups)
	return Group{}
}

func TestFindsTheRepeatedLinkBlock(t *testing.T) {
	groups := FindDuplicates(ParseRules(dashboardCSS))
	g := groupWith(t, groups, "text-decoration: none")

	var sels []string
	for _, r := range g.Rules {
		sels = append(sels, r.Selector)
	}
	require.ElementsMatch(t, []string{
		"#setup-section a:not(.btn)",
		".crumbs a",
		"a.kv-jump",
		".site-footer #server-version a",
	}, sels, "every selector carrying the identical body is named")

	// a.gh-slug carries an EXTRA declaration, so it is a different body and
	// must not be swept into the group -- the report has to be precise about
	// which rules are actually identical.
	require.NotContains(t, sels, "a.gh-slug")

	// Single-declaration hover bodies: four copies, well past the threshold.
	h := groupWith(t, groups, "text-decoration: underline")
	require.Len(t, h.Rules, 4)
}

func TestSingleDeclarationNeedsThreeCopies(t *testing.T) {
	twice := `.a { margin: 0; } .b { margin: 0; }`
	require.Empty(t, FindDuplicates(ParseRules(twice)), "two copies of a one-liner is noise")

	thrice := twice + ` .c { margin: 0; }`
	require.Len(t, FindDuplicates(ParseRules(thrice)), 1)
}

func TestLegitimateDuplicationIsNotReported(t *testing.T) {
	t.Run("across media contexts", func(t *testing.T) {
		src := `
.card { display: grid; gap: 1rem; }
@media (max-width: 40em) {
  .panel { display: grid; gap: 1rem; }
}`
		require.Empty(t, FindDuplicates(ParseRules(src)))
	})

	t.Run("keyframes from/to", func(t *testing.T) {
		src := `
@keyframes pulse {
  from { opacity: 1; transform: none; }
  50%  { opacity: 1; transform: none; }
  to   { opacity: 1; transform: none; }
}`
		require.Empty(t, FindDuplicates(ParseRules(src)))
	})

	t.Run("same block inside one media query is still a finding", func(t *testing.T) {
		src := `
@media (max-width: 40em) {
  .a { display: grid; gap: 1rem; }
  .b { display: grid; gap: 1rem; }
}`
		groups := FindDuplicates(ParseRules(src))
		require.Len(t, groups, 1)
		require.Equal(t, "@media (max-width: 40em)", groups[0].Context)
	})
}

func TestNormalizationMatchesWhatCSSCallsEqual(t *testing.T) {
	src := `
.a { COLOR:   var(--accent);text-decoration:none }
.b {
  color: var(--accent);
  text-decoration: none;
}
.c { color: var(--ACCENT); text-decoration: none; }`
	groups := FindDuplicates(ParseRules(src))
	require.Len(t, groups, 1, "case and whitespace differences are the same declaration")
	require.Len(t, groups[0].Rules, 2, "but a custom-property NAME is case-sensitive, so .c differs")
}

func TestParserSurvivesRealWorldSyntax(t *testing.T) {
	src := `
/* a comment with a } brace and a "quote */
@import url("x.css");
@layer base, utils;
.icon { background: url(data:image/svg+xml;base64,AAAA); content: "a;b"; }
.other { background: url(data:image/svg+xml;base64,AAAA); content: "a;b"; }
@font-face { font-family: X; src: url(x.woff2); }
@supports (display: grid) { .g { display: grid; gap: 0; } }
.unterminated { color: red;`
	rules := ParseRules(src)
	require.NotEmpty(t, rules)

	groups := FindDuplicates(rules)
	require.Len(t, groups, 1, "the url()/string semicolons must not split declarations")
	require.Equal(t, 2, len(groups[0].Rules))

	var sels []string
	for _, r := range rules {
		sels = append(sels, r.Selector)
	}
	require.Contains(t, sels, ".g", "@supports contents are ordinary rules")
	require.Contains(t, sels, "@font-face", "a declaration-holding at-rule is parsed as a block")
	require.Contains(t, sels, ".unterminated", "a truncated file still parses what it can")
}

func TestLineNumbersSurviveCommentsAndNesting(t *testing.T) {
	src := "/* one\n   two\n   three */\n.a { color: red; }\n@media print {\n  .b { color: red; }\n}\n"
	rules := ParseRules(src)
	require.Len(t, rules, 2)
	require.Equal(t, 4, rules[0].Line)
	require.Equal(t, 6, rules[1].Line)
}
