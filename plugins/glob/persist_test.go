package main

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestUTF16Len(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"h\u00e9llo", 5},   // precomposed e-acute is one UTF-16 unit
		{"e\u0301", 2},      // e + combining acute: two units
		{"日本語", 3},          // BMP CJK: one unit each
		{"😀", 2},            // astral plane: surrogate pair
		{"a😀b", 4},          //
		{"\U0001F600ok", 4}, // explicit astral escape
	}
	for _, tc := range cases {
		got := utf16Len(tc.in)
		assert.Equal(t, tc.want, got)

	}
}

func TestUTF16Slice(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"abcdef", 3, "abc"},
		{"abcdef", 0, ""},
		{"abcdef", -1, ""},
		{"abcdef", 99, "abcdef"},
		{"日本語", 2, "日本"},
		{"a😀b", 3, "a😀"},
		// Cut lands mid-surrogate-pair: the pair is dropped (JS would keep
		// a lone surrogate, which Go strings cannot represent).
		{"a😀b", 2, "a"},
		{"😀😀", 2, "😀"},
	}
	for _, tc := range cases {
		got := utf16Slice(tc.in, tc.n)
		assert.Equal(t, tc.want, got)

	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0 bytes"},
		{1, "1 bytes"}, // faithful to La: no singular special case
		{500, "500 bytes"},
		{1023, "1023 bytes"},
		{1024, "1KB"},
		{2000, "2KB"},     // 1.953 -> "2.0" -> ".0" stripped
		{25000, "24.4KB"}, // the spec's own example rendering
		{51200, "50KB"},
		{51300, "50.1KB"},
		{1048576, "1MB"},
		{20000000, "19.1MB"},
		{2000000000, "1.9GB"},
	}
	for _, tc := range cases {
		got := humanSize(tc.in)
		assert.Equal(t, tc.want, got)

	}
}

func TestSplitPreview(t *testing.T) {
	t.Run("short text passes through", func(t *testing.T) {
		p, more := splitPreview("hello\nworld", 2000)
		assert.False(t, p != "hello\nworld" || more)

	})
	t.Run("snaps to newline past half", func(t *testing.T) {
		text := strings.Repeat("a", 1500) + "\n" + strings.Repeat("b", 1000)
		p, more := splitPreview(text, 2000)
		assert.True(t, more)

		assert.False(t, utf16Len(p) != 1500 || !strings.HasSuffix(p, "a"))

	})
	t.Run("ignores newline before half", func(t *testing.T) {
		text := strings.Repeat("a", 500) + "\n" + strings.Repeat("b", 3000)
		p, more := splitPreview(text, 2000)
		assert.True(t, more)

		assert.Equal(t, 2000, utf16Len(p))

	})
	t.Run("no newline at all", func(t *testing.T) {
		p, _ := splitPreview(strings.Repeat("x", 5000), 2000)
		assert.Equal(t, 2000, utf16Len(p))

	})
}

func TestPersistOversizeUnderThresholdUnchanged(t *testing.T) {
	text := strings.Repeat("line\n", 10)
	got := persistOversize(text, "Glob", 50000, t.TempDir(), discardLogf)
	assert.Equal(t, text, got)

	// Exactly at the threshold: not persisted (strictly-greater check).
	exact := strings.Repeat("x", 100)
	got = persistOversize(exact, "Glob", 100, t.TempDir(), discardLogf)
	assert.Equal(t, exact, got)

}

var persistedPathRe = regexp.MustCompile(`Full output saved to: (.+)\n`)

func TestPersistOversizeFormat(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, strings.Repeat("p", 20))
	}
	text := strings.Join(lines, "\n") // 300*20 + 299 = 6299 chars
	got := persistOversize(text, "Glob", 5000, dir, discardLogf)

	m := persistedPathRe.FindStringSubmatch(got)
	require.NotNil(t, m)

	saved, err := os.ReadFile(m[1])
	require.Nil(t, err)

	assert.Equal(t, text, string(saved))

	assert.True(t, strings.HasPrefix(m[1], dir))

	// Preview: first 2000 UTF-16 units, snapped to the last newline
	// (every 21st char here, so the snap lands at 1994).
	preview, hasMore := splitPreview(text, persistPreviewChars)
	require.True(t, hasMore)

	want := "<persisted-output>\n" +
		"Output too large (6.2KB). Full output saved to: " + m[1] + "\n" +
		"\n" +
		"Preview (first 2KB):\n" +
		preview +
		"\n...\n" +
		"</persisted-output>"
	assert.Equal(t, want, got)

}

func TestPersistOversizeNoEllipsisWhenPreviewComplete(t *testing.T) {
	// Threshold below the preview size: the whole text fits in the
	// preview, so the "..." line is omitted (hasMore false).
	text := strings.Repeat("z", 150)
	got := persistOversize(text, "Glob", 100, t.TempDir(), discardLogf)
	assert.NotContains(t, got, "\n...\n")

	assert.True(t, strings.HasSuffix(got, text+"\n"+persistedOutputClose))

}

func TestPersistOversizeWriteFailureFallsBack(t *testing.T) {
	missing := t.TempDir() + "/does-not-exist"
	text := strings.Repeat("q", 200)
	logged := false
	logf := func(string, ...any) { logged = true }
	got := persistOversize(text, "Glob", 100, missing, logf)
	assert.Equal(t, text, got)

	assert.True(t, logged)

}

func TestPersistOversizeCountsUTF16Units(t *testing.T) {
	// 60 emoji = 120 UTF-16 units but 240 bytes; a threshold of 200
	// (bytes would exceed it) must NOT trigger persistence.
	text := strings.Repeat("😀", 60)
	got := persistOversize(text, "Glob", 200, t.TempDir(), discardLogf)
	assert.Equal(t, text, got)

	// 120 units > 100 threshold: persists.
	got = persistOversize(text, "Glob", 100, t.TempDir(), discardLogf)
	assert.NotEqual(t, text, got)

}
