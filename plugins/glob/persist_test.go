package main

import (
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
		if got := utf16Len(tc.in); got != tc.want {
			t.Errorf("utf16Len(%q) = %d, want %d", tc.in, got, tc.want)
		}
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
		if got := utf16Slice(tc.in, tc.n); got != tc.want {
			t.Errorf("utf16Slice(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
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
		if got := humanSize(tc.in); got != tc.want {
			t.Errorf("humanSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitPreview(t *testing.T) {
	t.Run("short text passes through", func(t *testing.T) {
		p, more := splitPreview("hello\nworld", 2000)
		if p != "hello\nworld" || more {
			t.Errorf("got %q, %v", p, more)
		}
	})
	t.Run("snaps to newline past half", func(t *testing.T) {
		text := strings.Repeat("a", 1500) + "\n" + strings.Repeat("b", 1000)
		p, more := splitPreview(text, 2000)
		if !more {
			t.Error("want hasMore")
		}
		if utf16Len(p) != 1500 || !strings.HasSuffix(p, "a") {
			t.Errorf("preview should cut at the newline (1500), got len %d", utf16Len(p))
		}
	})
	t.Run("ignores newline before half", func(t *testing.T) {
		text := strings.Repeat("a", 500) + "\n" + strings.Repeat("b", 3000)
		p, more := splitPreview(text, 2000)
		if !more {
			t.Error("want hasMore")
		}
		if utf16Len(p) != 2000 {
			t.Errorf("preview should hard-cut at 2000, got %d", utf16Len(p))
		}
	})
	t.Run("no newline at all", func(t *testing.T) {
		p, _ := splitPreview(strings.Repeat("x", 5000), 2000)
		if utf16Len(p) != 2000 {
			t.Errorf("got %d, want 2000", utf16Len(p))
		}
	})
}

func TestPersistOversizeUnderThresholdUnchanged(t *testing.T) {
	text := strings.Repeat("line\n", 10)
	if got := persistOversize(text, "Glob", 50000, t.TempDir(), discardLogf); got != text {
		t.Errorf("under-threshold text must pass through unchanged")
	}
	// Exactly at the threshold: not persisted (strictly-greater check).
	exact := strings.Repeat("x", 100)
	if got := persistOversize(exact, "Glob", 100, t.TempDir(), discardLogf); got != exact {
		t.Errorf("at-threshold text must pass through unchanged")
	}
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
	if m == nil {
		t.Fatalf("no persisted path in:\n%s", got)
	}
	saved, err := os.ReadFile(m[1])
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	if string(saved) != text {
		t.Error("persisted file must hold the full untruncated text")
	}
	if !strings.HasPrefix(m[1], dir) {
		t.Errorf("persisted file %q not under requested dir %q", m[1], dir)
	}

	// Preview: first 2000 UTF-16 units, snapped to the last newline
	// (every 21st char here, so the snap lands at 1994).
	preview, hasMore := splitPreview(text, persistPreviewChars)
	if !hasMore {
		t.Fatal("fixture must overflow the preview")
	}
	want := "<persisted-output>\n" +
		"Output too large (6.2KB). Full output saved to: " + m[1] + "\n" +
		"\n" +
		"Preview (first 2KB):\n" +
		preview +
		"\n...\n" +
		"</persisted-output>"
	if got != want {
		t.Errorf("persisted block mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPersistOversizeNoEllipsisWhenPreviewComplete(t *testing.T) {
	// Threshold below the preview size: the whole text fits in the
	// preview, so the "..." line is omitted (hasMore false).
	text := strings.Repeat("z", 150)
	got := persistOversize(text, "Glob", 100, t.TempDir(), discardLogf)
	if strings.Contains(got, "\n...\n") {
		t.Errorf("unexpected ellipsis line:\n%s", got)
	}
	if !strings.HasSuffix(got, text+"\n"+persistedOutputClose) {
		t.Errorf("block must end with preview, newline, close marker:\n%s", got)
	}
}

func TestPersistOversizeWriteFailureFallsBack(t *testing.T) {
	missing := t.TempDir() + "/does-not-exist"
	text := strings.Repeat("q", 200)
	logged := false
	logf := func(string, ...any) { logged = true }
	if got := persistOversize(text, "Glob", 100, missing, logf); got != text {
		t.Errorf("write failure must return the original text")
	}
	if !logged {
		t.Error("write failure should be logged")
	}
}

func TestPersistOversizeCountsUTF16Units(t *testing.T) {
	// 60 emoji = 120 UTF-16 units but 240 bytes; a threshold of 200
	// (bytes would exceed it) must NOT trigger persistence.
	text := strings.Repeat("😀", 60)
	if got := persistOversize(text, "Glob", 200, t.TempDir(), discardLogf); got != text {
		t.Error("threshold must be measured in UTF-16 units, not bytes")
	}
	// 120 units > 100 threshold: persists.
	if got := persistOversize(text, "Glob", 100, t.TempDir(), discardLogf); got == text {
		t.Error("120 UTF-16 units must exceed a threshold of 100")
	}
}
