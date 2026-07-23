// grepfwm.go implements the filenames_with_matches output mode, this
// plugin's redesigned default. The builtin's files_with_matches returned
// bare paths; this mode returns each file's matching lines too, grouped
// under a per-file header. ripgrep runs with --json so the grouping is
// unambiguous even for paths containing ":" or content that looks like a
// path; the events are grouped per file, files are ordered newest-first
// exactly like the filenames mode, lines ascend within a file, and
// head_limit/offset paginate the flattened stream of match/context LINES
// across all files (file headers and "--" separators are not counted).
// A file whose lines are entirely cut by pagination is omitted.
//
// Rendered shape (locked by tests; the two-space indent disambiguates
// headers from content, except for the pathological case of a filename
// that itself begins with two spaces — its header renders like a
// content line):
//
//	Found 2 files
//	newest.go:
//	  3:matched line
//	  4-context line
//	  --
//	  9:another match
//	older:colon.txt:
//	  1:hello
//
// With "-n": false the indent stays but the N:/N- prefixes are dropped.
// "--" separators appear between non-contiguous chunks within a file
// only when a context flag with a nonzero width is in effect, exactly
// like ripgrep's own printer (verified: -C 0 and flagless runs emit no
// separators).
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type fwmLine struct {
	num   int64
	text  string
	match bool
	// matchCol is the byte offset of the first match within text, or -1
	// for a context line or a line with no recorded match. It steers the
	// clamp window (clamp.go) so a match stays visible even far into a
	// very long line.
	matchCol int
}

type fwmGroup struct {
	path  string
	lines []fwmLine
}

// rgJSONText is rg's text-or-base64-bytes union for paths and line data.
type rgJSONText struct {
	Text  *string `json:"text"`
	Bytes *string `json:"bytes"`
}

func (t rgJSONText) value() string {
	if t.Text != nil {
		return *t.Text
	}
	if t.Bytes != nil {
		if b, err := base64.StdEncoding.DecodeString(*t.Bytes); err == nil {
			return string(b)
		}
	}
	return ""
}

type rgJSONEvent struct {
	Type string `json:"type"`
	Data struct {
		Path       rgJSONText `json:"path"`
		Lines      rgJSONText `json:"lines"`
		LineNumber *int64     `json:"line_number"`
		// Submatches carries each match's byte offsets within Lines.text;
		// only match events populate it. The first entry's Start steers
		// the clamp window for an over-long matching line.
		Submatches []struct {
			Start int64 `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

// parseFwmEvents groups rg --json match/context events per file in rg
// emission order. Unparseable lines (e.g. a final line truncated by the
// output cap or a timeout kill) are skipped.
func parseFwmEvents(lines []string) []*fwmGroup {
	var groups []*fwmGroup
	byPath := map[string]*fwmGroup{}
	for _, raw := range lines {
		var ev rgJSONEvent
		if json.Unmarshal([]byte(raw), &ev) != nil {
			continue
		}
		isMatch := ev.Type == "match"
		if !isMatch && ev.Type != "context" {
			continue
		}
		path := ev.Data.Path.value()
		if path == "" || ev.Data.LineNumber == nil {
			continue
		}
		grp := byPath[path]
		if grp == nil {
			grp = &fwmGroup{path: path}
			byPath[path] = grp
			groups = append(groups, grp)
		}
		matchByte := -1
		if isMatch && len(ev.Data.Submatches) > 0 {
			matchByte = int(ev.Data.Submatches[0].Start)
		}
		grp.lines = append(grp.lines, expandEventLines(ev.Data.Lines.value(), *ev.Data.LineNumber, isMatch, matchByte)...)
	}
	return groups
}

// expandEventLines splits one event's text (spanning several lines for
// multiline-mode matches) into individually numbered lines. Trailing
// \r\n / \n terminators are stripped from the rendered text (matching the
// shared runner's handling of rg's standard output). matchByte is the
// first submatch's byte offset within the whole event text (-1 for a
// context event); it is mapped onto whichever sub-line contains it so the
// clamp window can keep that match visible.
func expandEventLines(text string, firstNum int64, match bool, matchByte int) []fwmLine {
	if text == "" {
		return nil
	}
	parts := strings.SplitAfter(text, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	out := make([]fwmLine, 0, len(parts))
	byteOff := 0
	for i, part := range parts {
		content := strings.TrimSuffix(strings.TrimSuffix(part, "\n"), "\r")
		col := -1
		if match && matchByte >= byteOff && matchByte < byteOff+len(content) {
			col = matchByte - byteOff
		}
		out = append(out, fwmLine{
			num:      firstNum + int64(i),
			text:     content,
			match:    match,
			matchCol: col,
		})
		byteOff += len(part)
	}
	return out
}

// formatFilenamesWithMatches renders the amended default mode; see the
// file comment for the shape and pagination rules. The "Found N files"
// header and "No files found" empty text match the builtin's
// files_with_matches formatting, with N counting the files actually
// rendered after pagination.
func (g *grepTool) formatFilenamesWithMatches(rawLines []string, a *grepArgs) string {
	groups := parseFwmEvents(rawLines)
	paths := make([]string, len(groups))
	byPath := make(map[string]*fwmGroup, len(groups))
	for i, grp := range groups {
		paths[i] = grp.path
		byPath[grp.path] = grp
	}

	type ref struct {
		grp  *fwmGroup
		line fwmLine
	}
	var stream []ref
	for _, p := range sortPathsByMtimeDesc(paths) {
		grp := byPath[p]
		for _, ln := range grp.lines {
			stream = append(stream, ref{grp, ln})
		}
	}
	items, appliedLimit := paginate(stream, a.headLimit, a.offset)
	if len(items) == 0 {
		return "No files found"
	}

	separators := contextSeparatorsEnabled(a)
	numFiles := 0
	var body strings.Builder
	var cur *fwmGroup
	var prevNum int64
	for _, it := range items {
		if it.grp != cur {
			cur = it.grp
			numFiles++
			body.WriteString(g.displayPath(cur.path, a) + ":\n")
		} else if separators && it.line.num > prevNum+1 {
			body.WriteString("  --\n")
		}
		body.WriteString("  " + renderFwmLine(it.line, a.lineNums) + "\n")
		prevNum = it.line.num
	}

	head := fmt.Sprintf("Found %d %s", numFiles, plural(numFiles, "file"))
	if note := paginationNote(appliedLimit, a.offset); note != "" {
		head += " " + note
	}
	return head + "\n" + strings.TrimSuffix(body.String(), "\n")
}

// contextSeparatorsEnabled reports whether ripgrep's printer would be in
// context mode for the given flags: the effective flag (context beats -C
// beats -B/-A, the same precedence the argv uses) must carry a width
// greater than zero.
func contextSeparatorsEnabled(a *grepArgs) bool {
	switch {
	case a.context != nil:
		return *a.context > 0
	case a.dashC != nil:
		return *a.dashC > 0
	default:
		return (a.before != nil && *a.before > 0) || (a.after != nil && *a.after > 0)
	}
}

func renderFwmLine(ln fwmLine, lineNums bool) string {
	text := clampLine(ln.text, ln.matchCol)
	if !lineNums {
		return text
	}
	sep := "-"
	if ln.match {
		sep = ":"
	}
	return strconv.FormatInt(ln.num, 10) + sep + text
}
