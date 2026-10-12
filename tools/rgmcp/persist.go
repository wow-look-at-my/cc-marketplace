// Sizes are measured in UTF-16 code units to mirror JS String.length.
// Divergence from the builtin: the file lands under os.TempDir() instead
// of the session transcript's tool-results dir (an MCP server has neither
// the transcript dir nor the tool_use_id).
package rgmcp

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	PersistedOutputOpen  = "<persisted-output>"
	PersistedOutputClose = "</persisted-output>"
	PersistPreviewChars  = 2000
)

// UTF16Len mirrors JS String.prototype.length (UTF-16 code units).
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}

// When n lands in the middle of a surrogate pair, the pair is
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

func HumanSize(n int) string {
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

func SplitPreview(s string, limit int) (preview string, hasMore bool) {
	if UTF16Len(s) <= limit {
		return s, false
	}
	head := utf16Slice(s, limit)
	if i := strings.LastIndex(head, "\n"); i >= 0 && float64(UTF16Len(head[:i])) > float64(limit)*0.5 {
		return head[:i], true
	}
	return head, true
}

// On a write failure the full text is returned unchanged (divergence:
// the builtin has no such failure path worth mirroring; losing output
// would be worse).
func PersistOversize(text, filePrefix string, threshold int, tempDir string, logf func(string, ...any)) string {
	size := UTF16Len(text)
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

	preview, hasMore := SplitPreview(text, PersistPreviewChars)
	var b strings.Builder
	b.WriteString(PersistedOutputOpen + "\n")
	fmt.Fprintf(&b, "Output too large (%s). Full output saved to: %s\n\n", HumanSize(size), f.Name())
	fmt.Fprintf(&b, "Preview (first %s):\n", HumanSize(PersistPreviewChars))
	b.WriteString(preview)
	if hasMore {
		b.WriteString("\n...\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(PersistedOutputClose)
	return b.String()
}
