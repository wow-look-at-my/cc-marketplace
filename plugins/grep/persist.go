// persist.go ports claude-code's oversized-tool_result persistence
// (2.1.116:cli.js:164596-164679; format function verified byte-for-byte
// against 2.1.207's Ret/PBr/La): when the built result text exceeds the
// persistence ceiling, the full text is written to a file and the tool
// result becomes a <persisted-output> block with a ~2000-char preview.
//
// Sizes are measured in UTF-16 code units to mirror JS String.length.
// Divergence from the builtin: the file lands under os.TempDir() instead
// of the session transcript's tool-results dir (an MCP server has neither
// the transcript dir nor the tool_use_id).
//
// Tool-agnostic; a sibling plugin copies this file verbatim.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	persistedOutputOpen  = "<persisted-output>"
	persistedOutputClose = "</persisted-output>"
	// persistPreviewChars mirrors bzt = 2000 (preview size, UTF-16 units).
	persistPreviewChars = 2000
)

// utf16Len mirrors JS String.prototype.length (UTF-16 code units).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}

// utf16Slice mirrors JS String.prototype.slice(0, n) by UTF-16 code
// units. When n lands in the middle of a surrogate pair, the pair is
// dropped (Go strings cannot hold a lone surrogate).
func utf16Slice(s string, n int) string {
	if n <= 0 {
		return ""
	}
	u := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if u+w > n {
			return s[:i]
		}
		u += w
	}
	return s
}

// humanSize ports La (2.1.207:cli.js:18863-18870): "<n> bytes" under 1KB,
// then one-decimal KB/MB/GB with a trailing ".0" stripped.
func humanSize(n int) string {
	kb := float64(n) / 1024
	if kb < 1 {
		return fmt.Sprintf("%d bytes", n)
	}
	if kb < 1024 {
		return trimDotZero(kb) + "KB"
	}
	mb := kb / 1024
	if mb < 1024 {
		return trimDotZero(mb) + "MB"
	}
	return trimDotZero(mb/1024) + "GB"
}

func trimDotZero(f float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(f, 'f', 1, 64), ".0")
}

// splitPreview ports PBr (2.1.207:cli.js:202490-202496): take the first
// `limit` UTF-16 units; if the last newline inside sits past 50% of the
// limit, snap the cut to it.
func splitPreview(s string, limit int) (preview string, hasMore bool) {
	if utf16Len(s) <= limit {
		return s, false
	}
	head := utf16Slice(s, limit)
	if i := strings.LastIndex(head, "\n"); i >= 0 && float64(utf16Len(head[:i])) > float64(limit)*0.5 {
		return head[:i], true
	}
	return head, true
}

// persistOversize returns text unchanged while it fits within threshold
// UTF-16 units; otherwise it writes the full text to a temp file and
// returns the <persisted-output> block (format per Ret,
// 2.1.207:cli.js:202443-202460). On a write failure the full text is
// returned unchanged (divergence: the builtin has no such failure path
// worth mirroring; losing output would be worse).
func persistOversize(text, filePrefix string, threshold int, tempDir string, logf func(string, ...any)) string {
	size := utf16Len(text)
	if size <= threshold {
		return text
	}
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	f, err := os.CreateTemp(tempDir, filePrefix+"-*.txt")
	if err != nil {
		logf("persist tool result: %v", err)
		return text
	}
	_, werr := f.WriteString(text)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		logf("persist tool result to %s: write=%v close=%v", f.Name(), werr, cerr)
		return text
	}

	preview, hasMore := splitPreview(text, persistPreviewChars)
	var b strings.Builder
	b.WriteString(persistedOutputOpen + "\n")
	fmt.Fprintf(&b, "Output too large (%s). Full output saved to: %s\n\n", humanSize(size), f.Name())
	fmt.Fprintf(&b, "Preview (first %s):\n", humanSize(persistPreviewChars))
	b.WriteString(preview)
	if hasMore {
		b.WriteString("\n...\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(persistedOutputClose)
	return b.String()
}
