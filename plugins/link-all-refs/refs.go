// refs.go finds the references a message hands the reader with nothing to
// click: an issue or PR number, a commit SHA, a branch slug, a bare GitHub
// URL.
//
// The test is one step: remove every markdown link from the message, then look
// at what is left. Anything a matcher finds after that was never linked. There
// is no credit for a link given earlier in the message or in an earlier turn --
// the reader is looking at THIS text.
package main

import (
	"regexp"
	"slices"
	"strings"
)

// Ref is one unlinked reference: what it is, the token itself, and the line it
// sits on so the refusal can quote it back.
type Ref struct {
	Kind string
	Text string
	Line string
}

// A markdown link, with an optional title. Both halves go, anchor included:
// what survives the removal is by definition unlinked.
var mdLinkRe = regexp.MustCompile(`\[[^\]\n]*\]\([^)\s]+(?:[ \t]+"[^"]*")?\)`)

// An angle-bracket autolink, which every markdown renderer makes clickable.
var autoLinkRe = regexp.MustCompile(`<https?://[^>\s]+>`)

var (
	// #1234, with an optional owner/repo in front. The slug is part of the
	// match so `wow-look-at-my/go-toolchain#376` is reported whole, and so the
	// boundary check sees the start of the slug rather than the letter before
	// the `#`.
	numberRe = regexp.MustCompile(`(?:[A-Za-z0-9._-]+/[A-Za-z0-9._-]+)?#[0-9]{1,7}`)
	// A commit SHA: 7 to 40 hex characters. Validated further in refKind --
	// hex alone also describes a decimal number and an ordinary word.
	shaRe = regexp.MustCompile(`[0-9a-f]{7,40}`)
	// A branch slug. Matching on a conventional prefix keeps this off file
	// paths, which are the same shape: `go/core` is a directory, and no
	// heuristic separates it from a repository slug by looks alone. A bare
	// `owner/repo` with no number is therefore NOT matched -- the `#N` form
	// is, which is how a repository gets named in practice.
	branchRe = regexp.MustCompile(`(?i)(?:claude|feature|feat|fix|bugfix|hotfix|release|chore|refactor|wip|renovate|dependabot)/[A-Za-z0-9._/-]+`)
	// A GitHub URL that survived link removal is a URL the reader must select
	// and paste.
	urlRe = regexp.MustCompile(`https?://(?:www\.)?github\.com/[^\s)\]<>"']+`)
)

// FindUnlinked returns every reference in text that carries no link. Order
// follows the message; each distinct token is reported once.
func FindUnlinked(text string) []Ref {
	stripped := stripLinks(assertedText(text))
	var refs []Ref
	for _, m := range candidates(stripped) {
		if slices.ContainsFunc(refs, func(r Ref) bool { return r.Text == m.Text }) {
			continue
		}
		refs = append(refs, m)
	}
	return refs
}

// candidates runs every matcher over the text and keeps the hits that survive
// their kind's own validation and a boundary check.
func candidates(text string) []Ref {
	type matcher struct {
		kind  string
		re    *regexp.Regexp
		valid func(string) bool
	}
	matchers := []matcher{
		{"an issue or pull request number", numberRe, nil},
		{"a commit SHA", shaRe, validSHA},
		{"a branch", branchRe, validBranch},
		{"a bare GitHub URL", urlRe, nil},
	}
	var out []Ref
	for _, m := range matchers {
		for _, loc := range m.re.FindAllStringIndex(text, -1) {
			token := text[loc[0]:loc[1]]
			if !boundedToken(text, loc[0], loc[1]) {
				continue
			}
			if m.valid != nil && !m.valid(token) {
				continue
			}
			out = append(out, Ref{Kind: m.kind, Text: token, Line: lineAt(text, loc[0])})
		}
	}
	return out
}

// boundedToken reports whether a match stands on its own rather than sitting
// inside a longer word or path. A token glued to letters, digits, an
// underscore or a slash is part of something else -- a filename, an identifier,
// a URL path -- and naming it would be a false alarm.
func boundedToken(text string, start, end int) bool {
	if start > 0 && isTokenByte(text[start-1]) {
		return false
	}
	if end < len(text) && isTokenByte(text[end]) {
		return false
	}
	return true
}

func isTokenByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_', b == '/', b == '-', b == '.':
		return true
	}
	return false
}

// validSHA requires both a digit and a hex letter, which is what separates a
// commit from a decimal number (a timestamp, a byte count) and from an
// ordinary word spelled in a-f ("defaced", "cabbage").
func validSHA(token string) bool {
	hasDigit, hasLetter := false, false
	for _, r := range token {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r >= 'a' && r <= 'f':
			hasLetter = true
		}
	}
	return hasDigit && hasLetter
}

// validBranch requires a name after the prefix, so the bare word "fix/" is not
// a branch.
func validBranch(token string) bool {
	i := strings.Index(token, "/")
	return i > 0 && i < len(token)-1
}

// stripLinks removes markdown links and autolinks. A `&#N;` character
// reference goes too: it is XML, not an issue number, and the two are
// indistinguishable to the matcher.
func stripLinks(text string) string {
	text = mdLinkRe.ReplaceAllString(text, " ")
	text = autoLinkRe.ReplaceAllString(text, " ")
	return charRefRe.ReplaceAllString(text, " ")
}

var charRefRe = regexp.MustCompile(`&#x?[0-9a-fA-F]{1,7};`)

// assertedText drops what a message QUOTES rather than states: fenced code,
// indented code, and blockquotes. Writing this rule down means writing its
// examples down, and a guard that cannot survive its own documentation is a
// guard someone turns off.
//
// Inline backticks are NOT exempt. A SHA in backticks is the exact thing this
// plugin exists to catch.
func assertedText(text string) string {
	var out []string
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		if marker := fenceMarker(line); marker != "" {
			if fence == "" {
				fence = marker
			} else if marker == fence {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		if strings.HasPrefix(line, "    ") && trimmed != "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// fenceMarker returns "`" or "~" when a line opens or closes a code fence.
func fenceMarker(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, m) {
			return m[:1]
		}
	}
	return ""
}

// lineAt returns the line containing byte offset i, collapsed to one line and
// bounded so a refusal quotes a readable fragment rather than a paragraph.
func lineAt(text string, i int) string {
	start := strings.LastIndexByte(text[:i], '\n') + 1
	end := strings.IndexByte(text[i:], '\n')
	if end < 0 {
		end = len(text)
	} else {
		end += i
	}
	line := strings.Join(strings.Fields(text[start:end]), " ")
	if len(line) > 160 {
		line = line[:157] + "..."
	}
	return line
}
