package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestClampLineShort(t *testing.T) {
	// A line within budget is returned verbatim, with or without a column.
	assert.Equal(t, "hello", clampLine("hello", -1))
	assert.Equal(t, "hello", clampLine("hello", 2))
	// Exactly clampWidth runes is still verbatim (the boundary is inclusive).
	exact := strings.Repeat("a", clampWidth)
	assert.Equal(t, exact, clampLine(exact, -1))
}

func TestClampLineHeadAnchored(t *testing.T) {
	// No match column: the window anchors at the start, only the back is cut.
	line := strings.Repeat("a", clampWidth+50)
	got := clampLine(line, -1)
	assert.Equal(t, clampWidth, utf8.RuneCountInString(got))
	assert.False(t, strings.HasPrefix(got, ellipsis), "no leading ellipsis when anchored at the start")
	assert.True(t, strings.HasSuffix(got, ellipsis), "the back is cut")
	assert.Equal(t, strings.Repeat("a", clampWidth-1)+ellipsis, got)
}

func TestClampLineCenteredOnMatchInMiddle(t *testing.T) {
	// A match in the middle of a huge line: both ends are cut and the match
	// stays visible.
	line := strings.Repeat("a", 5000) + "NEEDLE" + strings.Repeat("b", 5000)
	got := clampLine(line, 5000) // byte offset of NEEDLE
	assert.True(t, strings.HasPrefix(got, ellipsis))
	assert.True(t, strings.HasSuffix(got, ellipsis))
	assert.Contains(t, got, "NEEDLE")
	assert.LessOrEqual(t, utf8.RuneCountInString(got), clampWidth)
}

func TestClampLineMatchNearEnd(t *testing.T) {
	// A match at the very end: the front is cut (leading ellipsis) and the
	// tail holding the match is kept, so there is no trailing ellipsis. This
	// is the "ellipsize the front" case.
	line := strings.Repeat("a", clampWidth+50) + "Z"
	got := clampLine(line, clampWidth+50) // byte offset of Z
	assert.True(t, strings.HasPrefix(got, ellipsis), "the front is cut")
	assert.True(t, strings.HasSuffix(got, "Z"), "the match at the end stays visible")
	assert.False(t, strings.HasSuffix(got, ellipsis))
	assert.LessOrEqual(t, utf8.RuneCountInString(got), clampWidth)
}

func TestRuneIndexOfByte(t *testing.T) {
	// "a" + U+00E9 (2 bytes) + " b": runes a(0) é(1) space(2) b(3).
	s := "a" + string(rune(0x00e9)) + " b"
	assert.Equal(t, 0, runeIndexOfByte(s, 0))   // 'a'
	assert.Equal(t, 1, runeIndexOfByte(s, 1))   // start of the 2-byte rune
	assert.Equal(t, 2, runeIndexOfByte(s, 3))   // the space, after the 2-byte rune
	assert.Equal(t, 3, runeIndexOfByte(s, 4))   // 'b'
	assert.Equal(t, 4, runeIndexOfByte(s, 100)) // past the end clamps to rune length
	assert.Equal(t, 0, runeIndexOfByte(s, -5))  // negative clamps to 0
}
