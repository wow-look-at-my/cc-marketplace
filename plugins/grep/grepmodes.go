// grepmodes.go ports the builtin's pagination helpers and per-mode
// post-processing/result-text builders (Q46/l46 at
// 2.1.116:cli.js:286174-286187, mode branches at 286373-286444, text
// builders at 286308-286335). The content, count, and filenames modes
// are byte-parity ports (filenames is the builtin's files_with_matches);
// the amended filenames_with_matches mode lives in grepfwm.go.
package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// paginate ports Q46: head_limit 0 means unlimited (only the offset
// applies and no limit is reported); otherwise the window is
// [offset, offset+limit) with the default limit 250, and the limit is
// reported as applied only when items beyond the window existed.
// JS Array.prototype.slice semantics are preserved via jsSlice so
// out-of-contract inputs (floats, negatives) behave identically.
func paginate[T any](items []T, headLimit *float64, offset float64) ([]T, *float64) {
	if headLimit != nil && *headLimit == 0 {
		return jsSlice(items, offset, float64(len(items))), nil
	}
	k := float64(defaultHeadLimit)
	if headLimit != nil {
		k = *headLimit
	}
	out := jsSlice(items, offset, offset+k)
	if float64(len(items))-offset > k {
		return out, &k
	}
	return out, nil
}

// jsSlice mirrors JS Array.prototype.slice(start, end): fractional
// indices truncate toward zero, negative indices count from the end, and
// everything clamps to [0, len].
func jsSlice[T any](items []T, start, end float64) []T {
	s := clampIndex(start, len(items))
	e := clampIndex(end, len(items))
	if e < s {
		e = s
	}
	return items[s:e]
}

func clampIndex(f float64, n int) int {
	t := math.Trunc(f)
	if math.IsNaN(t) {
		t = 0
	}
	if t < 0 {
		i := n + int(t)
		if i < 0 {
			return 0
		}
		return i
	}
	if t > float64(n) {
		return n
	}
	return int(t)
}

// paginationNote ports l46: "limit: N" and/or "offset: M" joined with
// ", " (the offset is only reported when positive, folding the
// builtin's appliedOffset spread condition into the same rule).
func paginationNote(appliedLimit *float64, offset float64) string {
	var parts []string
	if appliedLimit != nil {
		parts = append(parts, "limit: "+jsNumString(*appliedLimit))
	}
	if offset > 0 {
		parts = append(parts, "offset: "+jsNumString(offset))
	}
	return strings.Join(parts, ", ")
}

// jsNumString renders a number the way JS Number.prototype.toString does
// for the values this tool handles: integers without a decimal point,
// non-integers in their shortest decimal form.
func jsNumString(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// plural ports O6 (2.1.116:cli.js:33699-33701).
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// formatContent ports the content branch: paginate the raw rg lines,
// relativize the prefix before the FIRST colon of each line, join; empty
// content becomes "No matches found"; a pagination note is appended when
// a limit was applied or a positive offset given.
func (g *grepTool) formatContent(lines []string, a *grepArgs) string {
	items, appliedLimit := paginate(lines, a.headLimit, a.offset)
	mapped := make([]string, len(items))
	for i, l := range items {
		mapped[i] = relativizeColonPrefix(l, g.root, strings.Index(l, ":"))
	}
	body := strings.Join(mapped, "\n")
	if body == "" {
		body = "No matches found"
	}
	if note := paginationNote(appliedLimit, a.offset); note != "" {
		body += "\n\n[Showing results with pagination = " + note + "]"
	}
	return body
}

// formatCount ports the count branch: paginate the path:count lines,
// relativize the prefix before the LAST colon, sum the parseable counts,
// and always append the "Found N total occurrences across M files."
// trailer. The argv passes -c -H (the >=2.1.175 fix), so single-file
// searches keep their filename prefix and parse correctly.
func (g *grepTool) formatCount(lines []string, a *grepArgs) string {
	items, appliedLimit := paginate(lines, a.headLimit, a.offset)
	mapped := make([]string, len(items))
	numMatches, numFiles := 0, 0
	for i, l := range items {
		m := relativizeColonPrefix(l, g.root, strings.LastIndex(l, ":"))
		mapped[i] = m
		if r := strings.LastIndex(m, ":"); r > 0 {
			if n, ok := jsParseInt(m[r+1:]); ok {
				numMatches += n
				numFiles++
			}
		}
	}
	body := strings.Join(mapped, "\n")
	if body == "" {
		body = "No matches found"
	}
	trailer := fmt.Sprintf("\n\nFound %d total %s across %d %s.",
		numMatches, plural(numMatches, "occurrence"), numFiles, plural(numFiles, "file"))
	if note := paginationNote(appliedLimit, a.offset); note != "" {
		trailer += " with pagination = " + note
	}
	return body + trailer
}

// formatFilenames ports the builtin's files_with_matches branch
// verbatim: stat every path, sort newest-first (ties: ascending path
// compare), paginate the PATHS, relativize, and render "Found N files"
// over the list. This plugin exposes it under the name "filenames".
func (g *grepTool) formatFilenames(lines []string, a *grepArgs) string {
	sorted := sortPathsByMtimeDesc(lines)
	items, appliedLimit := paginate(sorted, a.headLimit, a.offset)
	if len(items) == 0 {
		return "No files found"
	}
	rel := make([]string, len(items))
	for i, p := range items {
		rel[i] = relativizePath(p, g.root)
	}
	head := fmt.Sprintf("Found %d %s", len(items), plural(len(items), "file"))
	if note := paginationNote(appliedLimit, a.offset); note != "" {
		head += " " + note
	}
	return head + "\n" + strings.Join(rel, "\n")
}

// relativizeColonPrefix applies the builtin's line mapping: when a colon
// exists past position 0, the prefix before it is relativized (QZH) and
// the rest of the line (colon included) is kept verbatim.
func relativizeColonPrefix(line, root string, colon int) string {
	if colon > 0 {
		return relativizePath(line[:colon], root) + line[colon:]
	}
	return line
}

// jsParseInt mirrors parseInt(s, 10): optional leading whitespace and
// sign, then a digit run (trailing garbage ignored); no digits means no
// number.
func jsParseInt(s string) (int, bool) {
	s = strings.TrimLeft(s, " \t\n\r\f\v")
	neg := false
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	j := 0
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:j])
	if err != nil {
		return 0, false
	}
	if neg {
		n = -n
	}
	return n, true
}

// sortPathsByMtimeDesc ports the files_with_matches comparator
// (2.1.116:cli.js:286432-286436): newest mtime first (millisecond
// precision, matching JS mtimeMs; failed stats sort as 0) with ties
// broken by ascending localeCompare order on the path (see collate.go).
// The stable sort mirrors JS Array.prototype.sort, so paths that collate
// equal keep rg's emission order.
func sortPathsByMtimeDesc(paths []string) []string {
	type entry struct {
		path  string
		mtime int64
	}
	entries := make([]entry, len(paths))
	for i, p := range paths {
		var mt int64
		if st, err := os.Stat(p); err == nil {
			mt = st.ModTime().UnixMilli()
		}
		entries[i] = entry{p, mt}
	}
	col := newPathCollator()
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].mtime != entries[j].mtime {
			return entries[i].mtime > entries[j].mtime
		}
		return col.CompareString(entries[i].path, entries[j].path) < 0
	})
	out := make([]string, len(paths))
	for i, e := range entries {
		out[i] = e.path
	}
	return out
}
