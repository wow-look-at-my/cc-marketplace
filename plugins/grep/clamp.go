// clamp.go bounds how much of a single matched or context line the grep
// tool renders. It supersedes the builtin's --max-columns 500 rule, which
// replaced any longer line with "[Omitted long matching line]" /
// "[Omitted long context line]". Per decree a matching line is never
// dropped -- only bounded: a line wider than clampWidth is rendered as a
// clampWidth-rune window with an ellipsis marking each cut edge, so the
// match itself always stays visible.
package main

import (
	"strings"
	"unicode/utf8"
)

// clampWidth is the maximum number of runes rendered for one line. The
// builtin omitted at 500 bytes; this shows the line, capped at ~4096 chars.
const clampWidth = 4096

// ellipsis marks an edge of a line that was cut to fit clampWidth. It is a
// single rune (U+2026 HORIZONTAL ELLIPSIS, built numerically so this source
// stays ASCII) and counts toward the budget.
const ellipsis = string(rune(0x2026))

// clampLine bounds text to clampWidth runes. A line within budget is
// returned verbatim. A longer line becomes a clampWidth-rune window:
//
//   - When matchByte >= 0 (the byte offset of the first match within text)
//     the window is centered on the match so it stays visible however far
//     into the line it sits -- cutting both the front and the back as
//     needed, each marked with an ellipsis.
//   - When matchByte < 0 (no known match position, e.g. a context line or
//     content-mode text) the window anchors at the start and only the back
//     is cut.
func clampLine(text string, matchByte int) string {
	if utf8.RuneCountInString(text) <= clampWidth {
		return text
	}
	runes := []rune(text)
	n := len(runes)

	// Reserve one rune per ellipsis that may be emitted: two when centering
	// (both edges can be cut), one when anchored at the start (back only).
	center := matchByte >= 0
	budget := clampWidth - 1
	if center {
		budget = clampWidth - 2
	}

	start := 0
	if center {
		start = runeIndexOfByte(text, matchByte) - budget/2
	}
	if start < 0 {
		start = 0
	}
	if start > n-budget {
		start = n - budget
	}
	end := start + budget

	var b strings.Builder
	if start > 0 {
		b.WriteString(ellipsis)
	}
	b.WriteString(string(runes[start:end]))
	if end < n {
		b.WriteString(ellipsis)
	}
	return b.String()
}

// runeIndexOfByte converts a byte offset in s to a rune index, clamped to
// s's rune length. An offset landing inside a multibyte rune snaps to that
// rune's start.
func runeIndexOfByte(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	if byteOff >= len(s) {
		return utf8.RuneCountInString(s)
	}
	return utf8.RuneCountInString(s[:byteOff])
}
