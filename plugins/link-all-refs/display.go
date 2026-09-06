// display.go rewrites a streaming message so every reference the reader might
// open arrives clickable.
//
// This is the whole enforcement. There is no Stop hook and nothing is sent back
// to the model, because a missing link is not work only the model can do: the
// token plus the checkout determine the URL, so the hook writes it. Asking the
// model to re-emit the same message with a link in it costs a round trip, and
// the message it writes to comply names the reference again while explaining
// itself, which trips the guard a second time. What the user reads at the end of
// that is a reply carrying nothing but links.
//
// displayContent is display-only, verified against the shipped bundle: the
// transcript and the model's next request are both fed from an array that is
// populated BEFORE this hook runs and never updated from its result. So this
// changes what the reader sees and nothing else, which is exactly the surface
// the rule is about.
package main

import (
	"strings"
)

// RewriteDelta returns the rendered form of one flush, and whether anything
// changed. A false return means print nothing, which leaves the CLI showing the
// original text.
func RewriteDelta(delta string, insideFence bool, res Resolver) (string, bool) {
	if delta == "" {
		return "", false
	}
	fence := insideFence
	changed := false
	lines := strings.Split(delta, "\n")

	for i, line := range lines {
		if fenceMarker(line) != "" {
			fence = !fence
			continue
		}
		if fence || isQuoted(line) {
			continue
		}
		rewritten, ok := rewriteLine(line, res)
		if !ok {
			continue
		}
		lines[i] = rewritten
		changed = true
	}

	if !changed {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

// rewriteLine splices a markdown link over every reference in one line that
// resolves to a page. A reference that does not resolve is left exactly as it
// was written -- see linkify.go on why a guessed URL is worse than plain text.
func rewriteLine(line string, res Resolver) (string, bool) {
	refs := FindUnlinkedInLine(line)
	if len(refs) == 0 {
		return "", false
	}
	out := line
	changed := false
	// Right to left, so an earlier offset stays valid after a later splice.
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		link, ok := Linkify(ref.Ref, res)
		if !ok {
			continue
		}
		out = out[:ref.Start] + link + out[ref.End:]
		changed = true
	}
	return out, changed
}

// isQuoted reports the two line shapes a message uses to quote rather than
// assert: a blockquote, and an indented code block. A message documenting this
// rule necessarily writes references down, and rewriting those would edit the
// example out from under the reader.
func isQuoted(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, ">") {
		return true
	}
	return strings.HasPrefix(line, "    ") && trimmed != ""
}

// EndsInsideFence reports whether text finishes inside a fenced code block. One
// flush cannot see the ``` that opened three lines earlier, so the state has to
// be carried across flushes.
func EndsInsideFence(text string) bool {
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		marker := fenceMarker(line)
		if marker == "" {
			continue
		}
		if fence == "" {
			fence = marker
		} else if marker == fence {
			fence = ""
		}
	}
	return fence != ""
}
