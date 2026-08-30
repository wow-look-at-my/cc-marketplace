// questions.go finds the two shapes a closing message must not end on: a
// question put to the user in prose, and a deferral that hands the user a
// decision without asking it through AskUserQuestion.
//
// Both tables are data on purpose. Extending either is editing one slice,
// never touching the matcher.
package main

import (
	"github.com/wow-look-at-my/go-containers/set"
	"regexp"
	"strings"
)

// deferralPhrases is the phrase table for handing a decision back without a
// question mark. Each entry offloads a choice the model was asked to make:
// "your call" and "let me know" park the work, "want me to" and "shall I" ask
// for permission already granted, "say the word" invents a confirmation
// gate. A message may state what it did and stop; it may not close by
// inviting the user to decide in prose.
var deferralPhrases = []string{
	"let me know",
	"your call",
	"up to you",
	"say the word",
	"just say",
	"tell me which",
	"tell me what you",
	"waiting on you",
	"waiting for you to",
	"if you want me to",
	"want me to",
	"would you like me to",
	"do you want me to",
	"shall i",
	"should i proceed",
	"i won't touch",
	"i will not touch",
	"and i'll pick",
	"and i will pick",
	"and i'll land",
	"or say",
}

// cueWords are the interrogative cues that separate a real question from a
// question mark doing another job. A "?" alone is not enough: this org's own
// specs are full of nullable types (`Int?`, `String?`) and every compare URL
// carries `?expand=1`, so matching a bare "?" reports a question in a message
// that asked nothing.
var cueWords = []string{
	"what", "which", "why", "how", "when", "where", "who", "whose",
	"should", "shall", "would", "could", "can", "will", "do", "does",
	"did", "is", "are", "was", "were", "am", "have", "has", "any",
	"want", "prefer", "ok", "okay", "right", "correct", "agree", "sound",
}

// Hit is one finding, with the line it sits on so the refusal can quote it.
type Hit struct {
	Kind string // "question" or "deferral"
	Text string
	Line string
}

type phraseMatcher struct {
	text string
	re   *regexp.Regexp
}

var deferralMatchers = compilePhrases(deferralPhrases)

func compilePhrases(phrases []string) []phraseMatcher {
	out := make([]phraseMatcher, 0, len(phrases))
	for _, p := range phrases {
		out = append(out, phraseMatcher{
			text: p,
			re:   regexp.MustCompile(`(?i)` + regexp.QuoteMeta(p)),
		})
	}
	return out
}

var cueSet = func() map[string]bool {
	m := make(map[string]bool, len(cueWords))
	for _, w := range cueWords {
		m[w] = true
	}
	return m
}()

// FindQuestions returns every question and deferral the message asserts in its
// own voice. An empty result allows the stop.
func FindQuestions(message string) []Hit {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	asserted := assertedText(message)
	scan, offsets := stripLinks(asserted)

	var hits []Hit
	seen := set.New[string]()

	for i := 0; i < len(scan); i++ {
		if scan[i] != '?' {
			continue
		}
		sentence := sentenceEndingAt(scan, i)
		if !closesAQuestion(scan, i, sentence) {
			continue
		}
		line := lineAt(asserted, offsets[i])
		if seen.Contains("q:" + line) {
			continue
		}
		seen.Add("q:" + line)
		hits = append(hits, Hit{Kind: "question", Text: strings.TrimSpace(sentence), Line: line})
	}

	for _, m := range deferralMatchers {
		loc := m.re.FindStringIndex(scan)
		if loc == nil {
			continue
		}
		line := lineAt(asserted, offsets[loc[0]])
		if seen.Contains("d:" + m.text) {
			continue
		}
		seen.Add("d:" + m.text)
		hits = append(hits, Hit{Kind: "deferral", Text: m.text, Line: line})
	}
	return hits
}

// sentenceEndingAt returns the sentence that the "?" at index i closes,
// bounded by the previous sentence terminator or line break.
func sentenceEndingAt(s string, i int) string {
	start := 0
	for j := i - 1; j >= 0; j-- {
		switch s[j] {
		case '.', '!', '?', '\n':
			start = j + 1
			j = -1
		}
	}
	sentence := s[start:i]
	if len(sentence) > 400 {
		sentence = sentence[len(sentence)-400:]
	}
	return sentence
}

// closesAQuestion decides whether the "?" at index i ends a question rather
// than spelling a nullable type.
//
// A "?" alone cannot tell them apart: `Int? n` and `raw_args?` have the same
// shape. Two things do. A question mark that ENDS ITS LINE is a question --
// a type annotation always has the thing it annotates after it. Otherwise the
// sentence must OPEN with an interrogative cue, which "The field is Int? and
// ..." does not, and "Is that a contract?" does.
func closesAQuestion(s string, i int, sentence string) bool {
	j := i + 1
	for j < len(s) && isTrailingCloser(s[j]) {
		j++
	}
	if j >= len(s) || s[j] == '\n' {
		return true
	}
	if !isASCIISpace(s[j]) {
		return false
	}
	return opensWithCue(sentence)
}

// isTrailingCloser reports a character that may sit between a question mark
// and the end of its line without changing that it ended the line.
func isTrailingCloser(c byte) bool {
	switch c {
	case '`', '"', '\'', ')', ']', '*', '_':
		return true
	}
	return false
}

// opensWithCue reports whether a sentence's first two words carry an
// interrogative cue. Two, not more: "The field is ..." reaches "is" on the
// third word, and that sentence is a statement.
func opensWithCue(sentence string) bool {
	fields := strings.Fields(strings.ToLower(sentence))
	for n, f := range fields {
		if n >= 2 {
			return false
		}
		if cueSet[strings.Trim(f, "\"'`*_,;:()[]{}<>-")] {
			return true
		}
	}
	return false
}

// stripLinks blanks markdown link destinations and bare URLs, returning the
// scannable text plus, for every byte kept, its offset in the input. A
// compare URL's `?expand=1` is not a question, and neither is a query string
// in a bare link.
func stripLinks(text string) (string, []int) {
	var b strings.Builder
	offsets := make([]int, 0, len(text))
	for i := 0; i < len(text); {
		if text[i] == ']' && i+1 < len(text) && text[i+1] == '(' {
			if end := strings.IndexByte(text[i+1:], ')'); end >= 0 {
				b.WriteString("](")
				offsets = append(offsets, i, i+1)
				i += end + 1
				continue
			}
		}
		if strings.HasPrefix(text[i:], "http://") || strings.HasPrefix(text[i:], "https://") {
			j := i
			for j < len(text) && !isASCIISpace(text[j]) && text[j] != ')' && text[j] != '>' {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(text[i])
		offsets = append(offsets, i)
		i++
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
// indented code, and blockquotes. A message documenting this very policy
// needs somewhere to write a question out. Ported from the sibling
// link-all-refs and no-blame-language plugins rather than reinvented.
//
// Inline backticks are NOT exempt, matching both siblings.
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
			out = append(out, "")
			continue
		}
		if fence != "" {
			out = append(out, "")
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, ">") {
			out = append(out, "")
			continue
		}
		if strings.HasPrefix(line, "    ") && trimmed != "" {
			out = append(out, "")
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

// lineAt returns the line containing byte offset i, collapsed and bounded so a
// refusal quotes a readable fragment rather than a paragraph.
func lineAt(text string, i int) string {
	if i < 0 || i > len(text) {
		return ""
	}
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
