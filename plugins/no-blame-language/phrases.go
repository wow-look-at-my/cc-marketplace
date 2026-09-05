// phrases.go finds the deflecting, blame-shifting phrases a closing message
// must not use: language that reports a defect instead of fixing it, or that
// shifts responsibility for code in this org's own repos onto some other
// author or an earlier point in time.
//
// bannedPhrases is a data table on purpose. Extending the list is editing one
// slice, never touching the matcher.
package main

import (
	"regexp"
	"strings"
)

// bannedPhrases is the phrase table. Every entry is grounded in this org's
// own written convention: "not mine to fix", "worth your attention",
// "flagging this for you", "left as-is", "someone should", "out of scope",
// "not caused by my change", and "you may want to" are the tells named
// verbatim in found-it-fix-it.md. "That predates this session", "this was
// existing code", "I only copied it", and "git blame shows" are the
// provenance openers named verbatim in you-wrote-it-own-it.md. "Pre-existing"
// and "preexisting" are the org owner's own explicit addition. The rest are
// direct synonyms of an entry already on the list.
var bannedPhrases = []string{
	"pre-existing",
	"preexisting",
	"not my fault",
	"not my problem",
	"not mine to fix",
	"not my responsibility",
	"worth your attention",
	"flagging this for you",
	"flagging this here",
	"left as-is",
	"someone should",
	"out of scope",
	"not caused by my change",
	"not caused by my diff",
	"you may want to",
	"that predates this session",
	"predates this session",
	"this was existing code",
	"i only copied it",
	"git blame shows",
	"not related to my change",
	"unrelated to my diff",

	// Refusing to engage. A turn may decline a request and say why, but it may
	// not close by announcing that the user's message will go unanswered --
	// that leaves the exchange stalled with nothing delivered.
	"not going to respond",
	"not responding to that",
	"won't respond",
	"will not respond",
	"refuse to respond",
	"declining to respond",
	"not going to answer that",
	"won't answer that",
	"will not answer that",
	"not going to engage",
	"won't engage",
	"will not engage",
	"not going to dignify",

	// Jargon the org owner has banned by name.
	"blast radius",
	"load bearing",
	"load-bearing",
}

// Hit is one banned phrase found in the message, with the line it sits on so
// the refusal can quote it back.
type Hit struct {
	Phrase string
	Line   string
}

// phraseMatcher pairs a banned phrase with its compiled, case-insensitive
// pattern, built once at startup rather than per call.
type phraseMatcher struct {
	text string
	re   *regexp.Regexp
}

var bannedPhraseMatchers = compilePhrases(bannedPhrases)

func compilePhrases(phrases []string) []phraseMatcher {
	out := make([]phraseMatcher, len(phrases))
	for i, p := range phrases {
		out[i] = phraseMatcher{text: p, re: regexp.MustCompile(`(?i)` + pattern(p))}
	}
	return out
}

// pattern quotes a phrase for literal matching, then widens every ASCII
// apostrophe to also match the typographic U+2019 a rendered message carries.
// The class is the same byte width in the pattern only; it matches a 3-byte
// rune in the subject without disturbing the offset table, since offsets are
// indexed by the match's start.
func pattern(phrase string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(phrase), "'", `['\x{2019}]`)
}

// FindBannedPhrases returns every banned phrase text contains, in table
// order, each reported once at its first occurrence. Runs of whitespace are
// collapsed to one space first, so a phrase a markdown line-wrap split across
// two lines still matches.
func FindBannedPhrases(text string) []Hit {
	filtered := assertedText(text)
	norm, offsets := normalizeWhitespace(filtered)
	var hits []Hit
	for _, pm := range bannedPhraseMatchers {
		loc := pm.re.FindStringIndex(norm)
		if loc == nil {
			continue
		}
		hits = append(hits, Hit{Phrase: pm.text, Line: lineAt(filtered, offsets[loc[0]])})
	}
	return hits
}

// normalizeWhitespace collapses every run of ASCII whitespace in text to a
// single space. offsets[i] is the byte offset in text that produced byte i of
// the result, so a match found in the normalized string can be traced back to
// the line it came from.
func normalizeWhitespace(text string) (norm string, offsets []int) {
	var b strings.Builder
	inSpace := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if isASCIISpace(c) {
			if !inSpace {
				b.WriteByte(' ')
				offsets = append(offsets, i)
				inSpace = true
			}
			continue
		}
		inSpace = false
		b.WriteByte(c)
		offsets = append(offsets, i)
	}
	return b.String(), offsets
}

func isASCIISpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// assertedText drops what a message QUOTES rather than states: fenced code,
// indented code, and blockquotes. A message documenting this very policy, or
// explaining what a banned phrase is, needs somewhere to write the phrase out
// -- a guard that cannot survive its own documentation is a guard someone
// turns off.
//
// Inline backticks are NOT exempt. A deflecting phrase in backticks is the
// exact thing this plugin exists to catch.
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
