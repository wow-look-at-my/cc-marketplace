package main

import (
	"regexp"
	"strings"
)

// LENGTH is checked as PROPORTION, never as a number on its own.
//
// A long comment on something that needs it is correct, and truncating one
// that carries real information is the same defect in the other direction. The
// failure worth catching is the manual wearing a comment's clothes: nineteen
// lines and a hundred and ninety words explaining a string constant -- which
// nobody reading that constant next year needs, and which is re-read on every
// pass through the file forever.
//
// So the test is the comment against the THING IT ANNOTATES. A page of prose
// over a subtle function is fine. The same page over `const X = "..."` is a
// document that has not been written yet.

const (
	// Where a comment stops being an explanation and becomes a document. The
	// case this is drawn from ran to 19 lines and 189 words on one constant.
	bloatLines = 8
	bloatWords = 90
)

// trivialDeclRe matches the declarations that cannot justify a long comment: a
// constant, a variable, a struct field, a simple assignment -- anything whose
// whole meaning is on one line.
var trivialDeclRe = regexp.MustCompile(
	`^\s*(?:(?:export\s+)?(?:const|var|let|static|final)\s+\w+\s*[:=]|` + // const X = / var x: T =
		`\w+\s*(?::\s*[\w\[\]<>., *]+)?\s*=[^=]|` + // x = / x: T =
		`\w+\s+[\w\[\]<>., *]+\s+` + "`" + `|` + // a tagged struct field
		`\w+\s+(?:string|u?int\d*|float\d*|bool|byte|rune|error|time\.\w+)\s*$)`) // a plain struct field

// bloated reports whether a block is out of proportion to what it annotates,
// and why.
func bloated(b Block) (string, bool) {
	lines := countProseLines(b.Text)
	words := len(strings.Fields(b.Text))
	if lines < bloatLines && words < bloatWords {
		return "", false
	}

	first := firstCodeLine(b.Code)
	if first == "" || !trivialDeclRe.MatchString(first) {
		// Attached to something substantial -- a function, a type, a whole
		// file. Length there is a judgement call, not a defect.
		return "", false
	}
	// A declaration that spans lines is not the trivial case.
	if strings.Count(strings.TrimSpace(b.Code), "\n") > 0 && strings.HasSuffix(strings.TrimSpace(first), "{") {
		return "", false
	}
	return trim(first), true
}

func countProseLines(text string) int {
	n := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

func firstCodeLine(code string) string {
	for _, l := range strings.Split(code, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 70 {
		return s[:70] + "…"
	}
	return s
}
