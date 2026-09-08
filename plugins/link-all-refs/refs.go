// refs.go finds the references a message hands the reader with nothing to
// click: an issue or PR number, a commit SHA, a branch slug, a bare GitHub URL.
//
// Each match carries the byte range it occupies, because the caller rewrites
// the reference in place rather than reporting it. Text that is already a link
// is blanked first, so a correct reference is never rewritten twice.
package main

import (
	"regexp"
	"slices"
	"strings"
)

// Ref is one unlinked reference: what it is, and the token itself.
type Ref struct {
	Kind string
	Text string
	// Backticked is true when the token sits inside a single pair of inline
	// backticks. The rewrite must then keep the backticks INSIDE the link text
	// (`` [`text`](url) ``, not `` `[text](url)` ``) -- markdown does not render a
	// link that opens inside a code span and closes outside it, and the reader is
	// left with the raw brackets instead of a clickable link.
	Backticked bool
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
	// The last character may not be a '.' or a '/': git forbids a ref that ends
	// in either, so a trailing one belongs to the sentence rather than the
	// branch. Without that the match swallows the full stop in
	// `pushed claude/thing.` and the rendered link 404s.
	branchRe = regexp.MustCompile(`(?i)(?:claude|feature|feat|fix|bugfix|hotfix|release|chore|refactor|wip|renovate|dependabot)/[A-Za-z0-9._/-]*[A-Za-z0-9_-]`)
	// A GitHub URL that survived link removal is a URL the reader must select
	// and paste.
	urlRe = regexp.MustCompile(`https?://(?:www\.)?github\.com/[^\s)\]<>"']+`)
)

// Located is one unlinked reference with the byte range it occupies, so a
// caller can splice a link in over it.
type Located struct {
	Ref
	Start int
	End   int
}

// FindUnlinkedInLine returns every unlinked reference in one line of prose,
// with offsets into that same line. Every token is reported, including a repeat:
// the caller rewrites each occurrence, rather than naming the token once.
//
// Text that is already a link is blanked first. Without that, the branch matcher
// fires on the `claude/x` inside `[claude/x](url)` and the URL matcher fires on
// the target, so the rewrite nests a link inside a link.
//
// BLANKED, not stripped: each link becomes an equal run of spaces. A replacement
// that changes the length moves every offset after it, and a moved offset
// splices the next link into the middle of a word.
func FindUnlinkedInLine(line string) []Located {
	return candidates(blankLinks(line))
}

// blankLinks replaces every markdown link, autolink and character reference
// with spaces, preserving the length of the text exactly.
func blankLinks(text string) string {
	b := []byte(text)
	for _, re := range []*regexp.Regexp{mdLinkRe, autoLinkRe, charRefRe} {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			for i := loc[0]; i < loc[1]; i++ {
				b[i] = ' '
			}
		}
	}
	return string(b)
}

// candidates runs every matcher over the text and keeps the hits that survive
// their kind's own validation and a boundary check. Each hit carries the byte
// range it occupies, so a caller can splice over it.
//
// Overlaps are dropped: a bare GitHub URL contains a slug that also matches the
// branch matcher, and rewriting both would nest one link inside another.
func candidates(text string) []Located {
	type matcher struct {
		kind  string
		re    *regexp.Regexp
		valid func(string) bool
	}
	// URL first: it is the longest match, and claiming its span here is what
	// stops the branch matcher from firing inside it.
	matchers := []matcher{
		{"a bare GitHub URL", urlRe, nil},
		{"an issue or pull request number", numberRe, nil},
		{"a commit SHA", shaRe, validSHA},
		{"a branch", branchRe, validBranch},
	}
	var out []Located
	for _, m := range matchers {
		for _, loc := range m.re.FindAllStringIndex(text, -1) {
			token := text[loc[0]:loc[1]]
			if !boundedToken(text, loc[0], loc[1]) {
				continue
			}
			if m.valid != nil && !m.valid(token) {
				continue
			}
			if slices.ContainsFunc(out, func(o Located) bool { return loc[0] < o.End && o.Start < loc[1] }) {
				continue
			}
			start, end, backticked := backtickWrapped(text, loc[0], loc[1])
			out = append(out, Located{
				Ref:   Ref{Kind: m.kind, Text: token, Backticked: backticked},
				Start: start,
				End:   end,
			})
		}
	}
	slices.SortStableFunc(out, func(a, b Located) int { return a.Start - b.Start })
	return out
}

// boundedToken reports whether a match stands on its own rather than sitting
// inside a longer word or path. A token glued to letters, digits, an
// underscore or a slash is part of something else -- a filename, an identifier,
// a URL path -- and naming it would be a false alarm.
func boundedToken(text string, start, end int) bool {
	if start > 0 && continuesToken(text, start-1, -1) {
		return false
	}
	if end < len(text) && continuesToken(text, end, +1) {
		return false
	}
	return true
}

// continuesToken reports whether the byte at i extends the match into a longer
// word, reading away from it in direction dir.
//
// A '.' is the whole subtlety. It joins a filename to its extension, so
// `6884dd2abc.log` must not be reported as a commit -- but it is also how a
// sentence ends, and treating every trailing '.' as part of the token made
// every reference that ENDS a sentence invisible to this guard. So a '.' counts
// only when an alphanumeric follows it in the same direction. `6884dd2.` at the
// end of a sentence is a commit; `6884dd2abc.log` is a filename.
func continuesToken(text string, i, dir int) bool {
	b := text[i]
	if b == '.' {
		next := i + dir
		return next >= 0 && next < len(text) && isAlnum(text[next])
	}
	return isAlnum(b) || b == '_' || b == '/' || b == '-'
}

// backtickWrapped reports whether the match at [start,end) sits inside one pair
// of inline backticks, and returns the widened range that swallows them.
//
// A double backtick (``x``) is the escape a code span uses to hold a literal
// backtick, not a wrap around this token, so only a LONE backtick on each side
// counts. Widening the range here is what lets the caller drop the original
// backticks and put fresh ones inside the link text instead.
func backtickWrapped(text string, start, end int) (int, int, bool) {
	if start == 0 || end >= len(text) || text[start-1] != '`' || text[end] != '`' {
		return start, end, false
	}
	if start >= 2 && text[start-2] == '`' {
		return start, end, false
	}
	if end+1 < len(text) && text[end+1] == '`' {
		return start, end, false
	}
	return start - 1, end + 1, true
}

func isAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
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

// Linked is one markdown link the text already carries, with the byte range of
// the whole construct and the words inside it.
//
// The unlinked matchers never see these, because blankLinks wipes them first.
// That is right for adding a link and wrong for removing one, so a caller that
// wants to strip a link asks for it here instead.
type Linked struct {
	// Text is what survives the strip: the link text, or the bare URL for an
	// autolink. The words stay and only the wrapper goes.
	Text string
	// URL is the target, which is what decides whether the link is dead.
	URL   string
	Start int
	End   int
}

// linkPartsRe is mdLinkRe with the text and the target captured.
var linkPartsRe = regexp.MustCompile(`\[([^\]\n]*)\]\(([^)\s]+)(?:[ \t]+"[^"]*")?\)`)

// FindLinksInLine returns every markdown link and autolink in one line, with the
// byte range each occupies. The ranges are taken from the original text, so a
// caller can splice over them directly.
func FindLinksInLine(line string) []Linked {
	var out []Linked
	for _, m := range linkPartsRe.FindAllStringSubmatchIndex(line, -1) {
		out = append(out, Linked{
			Text:  line[m[2]:m[3]],
			URL:   line[m[4]:m[5]],
			Start: m[0],
			End:   m[1],
		})
	}
	for _, loc := range autoLinkRe.FindAllStringIndex(line, -1) {
		// An autolink has no separate text. Unwrapping it leaves the URL,
		// which is the same words with the angle brackets gone.
		url := line[loc[0]+1 : loc[1]-1]
		out = append(out, Linked{Text: url, URL: url, Start: loc[0], End: loc[1]})
	}
	slices.SortStableFunc(out, func(a, b Linked) int { return a.Start - b.Start })
	return out
}

// issueURLRe matches a GitHub pull request or issue URL. A trailing path or
// anchor is allowed and ignored: a link to one file of a merged pull request is
// just as dead as a link to the pull request itself.
//
// `pull` and `issues` are one case on purpose. GitHub serves a pull request
// under both spellings, and a closed issue has nothing left to do either.
var issueURLRe = regexp.MustCompile(`^https?://(?:www\.)?github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)/(?:pull|issues)/([0-9]{1,7})(?:[/#?].*)?$`)

// IssueRef reads the repository and number out of a GitHub pull request or issue
// URL, and reports false for every other URL.
func IssueRef(url string) (Repo, string, bool) {
	m := issueURLRe.FindStringSubmatch(strings.TrimRight(url, "."))
	if m == nil {
		return Repo{}, "", false
	}
	return Repo{Owner: m[1], Name: m[2]}, m[3], true
}

// charRefRe is a `&#N;` character reference. It is XML, not an issue number,
// and the two are indistinguishable to the matcher, so it is blanked alongside
// the links.
var charRefRe = regexp.MustCompile(`&#x?[0-9a-fA-F]{1,7};`)

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
