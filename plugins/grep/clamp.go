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

// clampLine bounds text to clampWidth runes. A line within budget stays
// verbatim. A longer line becomes a clampWidth-rune window. An ellipsis
// marks each cut edge. A matchByte of zero or more is the byte offset of
// the first match. The window then centers on that match. A negative
// matchByte gives no match position. The window then starts at the first
// rune and cuts only the back.
func clampLine(text string, matchByte int) string {
	if utf8.RuneCountInString(text) <= clampWidth {
		return text
	}
	runes := []rune(text)
	n := len(runes)

	// Each ellipsis needs one rune of the budget. A centered window can cut
	// both edges. It reserves two runes. An anchored window reserves one.
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
