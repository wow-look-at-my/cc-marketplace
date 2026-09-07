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
	return fmt.Sprintf("\n\n> **ask-properly** -- %s%s. That is a decision handed over in prose. "+
		"Answer it yourself and say what you assumed, or ask it with AskUserQuestion: recommendation "+
		"first and labelled, and every option saying what it costs and what it buys.",
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
