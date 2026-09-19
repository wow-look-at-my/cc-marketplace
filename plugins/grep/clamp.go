// clamp.go bounds how much of a single matched or context line the grep
// tool renders. Per decree a matching line is never dropped -- only
// bounded: a line wider than clampWidth is rendered as a clampWidth-rune
// window with an ellipsis marking each cut edge, so the match itself
// always stays visible.
package main

import (
	"strings"
	"unicode/utf8"
)

// clampWidth is the maximum number of runes rendered for a single line.
const clampWidth = 4096

// ellipsis marks an edge of a line that was cut to fit clampWidth.
const ellipsis = string(rune(0x2026))

// clampLine bounds text to clampWidth runes. A line within budget is
// returned verbatim. A longer line becomes a clampWidth-rune window:
//
//	a context line or content-mode text) the window anchors at the start
//	  and only the back is cut.
func clampLine(text string, matchByte int) string {
	if utf8.RuneCountInString(text) <= clampWidth {
		return text
	}
	runes := []rune(text)
	n := len(runes)

	// Reserve a single rune per ellipsis that may be emitted: when
	// centering (both edges can be cut), a single when anchored at the start (back only).
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
