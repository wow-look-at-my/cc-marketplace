// display.go turns the findings into the one line appended to a finished
// message.
//
// This is the whole enforcement. Nothing is refused and nothing is sent back to
// the model, because the reader is who the rule serves: a deflection the reader
// can see marked as a deflection has already cost the writer what it was meant
// to cost. Asking the model to re-emit the message instead costs a round trip,
// and the message it writes to comply names the phrase again while explaining
// itself, which trips the guard a second time.
package main

import (
	"fmt"
	"strings"
)

// annotationCap bounds how many phrases the line names. The line sits under the
// message the reader just read, so it has to stay one line.
const annotationCap = 3

// Annotate returns the text to append to a finished message, or "" when the
// message deflects nothing. The leading blank line separates it from whatever
// the message ended on, and the blockquote marks it as the hook talking rather
// than the model.
func Annotate(message string) string {
	hits := FindBannedPhrases(message)
	if len(hits) == 0 {
		return ""
	}
	quoted := make([]string, 0, annotationCap)
	for _, hit := range hits[:min(len(hits), annotationCap)] {
		quoted = append(quoted, fmt.Sprintf("%q", hit.Phrase))
	}
	more := ""
	if len(hits) > annotationCap {
		more = ", and more"
	}
	return fmt.Sprintf("\n\n> **no-blame-language** -- %s%s. This reports a defect instead of owning it. "+
		"Fix the root cause and say so, or state precisely what you found, why it is not yours to fix, "+
		"and what you did instead.", strings.Join(quoted, ", "), more)
}
