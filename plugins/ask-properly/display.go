// display.go turns the findings into the one line appended to a finished
// message.
//
// This is the whole enforcement. Nothing is refused and nothing is sent back to
// the model, because the reader is the person the question was aimed at: a
// prose question the reader can see marked as an offloaded decision has already
// cost the writer what it was meant to cost. Asking the model to re-emit the
// message instead costs a round trip, and the message it writes to comply puts
// the decision back into prose while explaining itself, which trips the guard a
// second time.
package main

import (
	"fmt"
	"strings"
)

// annotationCap bounds how many findings the line names. The line sits under
// the message the reader just read, so it has to stay one line.
const annotationCap = 3

// Annotate returns the text to append to a finished message, or "" when the
// message hands over no decision. The leading blank line separates it from
// whatever the message ended on, and the blockquote marks it as the hook
// talking rather than the model.
func Annotate(message string) string {
	hits := FindQuestions(message)
	if len(hits) == 0 {
		return ""
	}
	// A deferral phrase often sits inside a question already quoted ("Want me
	// to fix it?" carries "want me to"). Naming both says the same thing twice
	// on the reader's screen.
	texts := make([]string, 0, len(hits))
	for _, hit := range hits {
		text := finding(hit)
		if hit.Kind == "deferral" && containsFold(texts, text) {
			continue
		}
		texts = append(texts, text)
	}

	quoted := make([]string, 0, annotationCap)
	for _, text := range texts[:min(len(texts), annotationCap)] {
		quoted = append(quoted, fmt.Sprintf("%q", text))
	}
	more := ""
	if len(texts) > annotationCap {
		more = ", and more"
	}
	// The repair is DECIDE, not "ask on a card instead". A card is a slower
	// stall: the work stops either way, and the owner is answering a question
	// he did not want. His words on being pointed at one: "the opposite of what
	// i want". Every change here lands on one branch he can delete in seconds,
	// so a wrong guess is cheap and a stall costs the session. A card is right
	// only where guessing is not: an action outside the branch, a destructive
	// one, access this session lacks, or a fork the owner reserved.
	return fmt.Sprintf("\n\n> **ask-properly** -- %s%s. That is a decision handed over in prose. "+
		"Make it yourself and say what you assumed. A card is for the narrow "+
		"cases guessing cannot cover: reaching outside this branch, destroying "+
		"something, access you lack, or a fork the owner reserved.",
		strings.Join(quoted, ", "), more)
}

// containsFold reports whether any of texts already carries needle, ignoring
// case.
func containsFold(texts []string, needle string) bool {
	for _, text := range texts {
		if strings.Contains(strings.ToLower(text), strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

// findingCap bounds one quoted finding. A question hit carries its whole
// sentence, and a paragraph quoted back under the paragraph it came from is
// noise rather than a pointer.
const findingCap = 80

// finding renders one hit for the annotation. A question hit's text is the
// sentence WITHOUT the "?" that closed it, because that is where the detector
// cut it, so the mark goes back on.
func finding(hit Hit) string {
	text := strings.TrimSpace(hit.Text)
	if hit.Kind == "question" {
		text += "?"
	}
	if len(text) > findingCap {
		text = "..." + text[len(text)-findingCap:]
	}
	return text
}
