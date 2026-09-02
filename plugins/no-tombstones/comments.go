// comments.go pulls out the only text this plugin judges: the prose a write
// adds. In source that is the comments, never the code, so a string literal
// holding the word "previously" is not a tombstone. In a document it is the
// prose, with code fences and frontmatter skipped.
//
// The input is a fragment, not a file: an Edit's new_string starts wherever the
// model put it. A scanner starting mid-string can therefore read code as a
// comment. That costs a false positive at worst, and the alternative -- reading
// the whole file off disk and locating the fragment in it -- buys precision the
// deny does not need.
package main

import (
	"path/filepath"
	"strings"
)

// Block is one run of comment lines, or one paragraph of a document.
type Block struct {
	Text  string
	Lines int
}

// style says how a language spells a comment. A language with no block form
// leaves blockOpen empty.
type style struct {
	line       []string
	blockOpen  string
	blockClose string
	strings    []byte // quote characters that start a literal
	raw        byte   // a quote character whose literal ignores backslashes
}

var (
	cLike   = style{line: []string{"//"}, blockOpen: "/*", blockClose: "*/", strings: []byte{'"', '\''}, raw: '`'}
	hashish = style{line: []string{"#"}, strings: []byte{'"', '\''}}
	dashish = style{line: []string{"--"}, strings: []byte{'"', '\''}}
	lispish = style{line: []string{";"}, strings: []byte{'"'}}
)

// byExt maps a file extension to its comment spelling. An extension absent from
// this table is not judged at all: guessing at a syntax risks reading code as
// prose, and a deny built on that is the kind a user turns off.
var byExt = map[string]style{
	".go": cLike, ".c": cLike, ".h": cLike, ".cc": cLike, ".cpp": cLike,
	".hpp": cLike, ".hh": cLike, ".cxx": cLike, ".m": cLike, ".mm": cLike,
	".java": cLike, ".js": cLike, ".mjs": cLike, ".cjs": cLike, ".jsx": cLike,
	".ts": cLike, ".tsx": cLike, ".mts": cLike, ".cts": cLike, ".rs": cLike,
	".swift": cLike, ".kt": cLike, ".kts": cLike, ".cs": cLike, ".scala": cLike,
	".php": cLike, ".dart": cLike, ".zig": cLike, ".glsl": cLike, ".wgsl": cLike,
	".vert": cLike, ".frag": cLike, ".comp": cLike, ".proto": cLike, ".jq": cLike,

	".sh": hashish, ".bash": hashish, ".zsh": hashish, ".fish": hashish,
	".rb": hashish, ".pl": hashish, ".r": hashish, ".yml": hashish,
	".yaml": hashish, ".toml": hashish, ".tf": hashish, ".just": hashish,
	".dats": hashish, ".cmake": hashish, ".mk": hashish,

	".sql": dashish, ".lua": dashish, ".hs": dashish, ".elm": dashish,
	".ads": dashish, ".adb": dashish,

	".el": lispish, ".lisp": lispish, ".clj": lispish, ".scm": lispish,
}

// byBase covers the extensionless files that still carry comments.
var byBase = map[string]style{
	"makefile": hashish, "dockerfile": hashish, "justfile": hashish,
	"containerfile": hashish, ".gitignore": hashish, ".dockerignore": hashish,
}

// AddedBlocks returns the prose a write adds to path, or nil when the path is
// one this plugin does not judge.
func AddedBlocks(path, added string) []Block {
	if IsDocument(path) {
		return paragraphs(added)
	}
	st, ok := styleFor(path)
	if !ok {
		return nil
	}
	return commentBlocks(added, st)
}

func styleFor(path string) (style, bool) {
	base := strings.ToLower(filepath.Base(path))
	if st, ok := byBase[base]; ok {
		return st, true
	}
	st, ok := byExt[strings.ToLower(filepath.Ext(path))]
	return st, ok
}

// IsDocument reports whether path names prose rather than source.
func IsDocument(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".rst", ".txt", ".adoc":
		return true
	}
	return false
}

// commentBlocks walks src and collects each comment, merging a run of adjacent
// line comments into one block so the volume cap sees the essay rather than its
// individual lines.
func commentBlocks(src string, st style) []Block {
	type piece struct {
		line int
		text string
	}
	var pieces []piece

	lines := strings.Split(src, "\n")
	inBlock := false
	var block []string
	blockStart := 0

	for n, line := range lines {
		rest := line
		if inBlock {
			if i := strings.Index(rest, st.blockClose); i >= 0 {
				block = append(block, rest[:i])
				pieces = append(pieces, piece{blockStart, strings.Join(block, "\n")})
				block, inBlock = nil, false
				rest = rest[i+len(st.blockClose):]
			} else {
				block = append(block, rest)
				continue
			}
		}
		for {
			at, kind := nextMarker(rest, st)
			if at < 0 {
				break
			}
			if kind == markerBlock {
				open := rest[at+len(st.blockOpen):]
				if i := strings.Index(open, st.blockClose); i >= 0 {
					pieces = append(pieces, piece{n, open[:i]})
					rest = open[i+len(st.blockClose):]
					continue
				}
				inBlock, blockStart, block = true, n, []string{open}
			} else {
				pieces = append(pieces, piece{n, rest[at:]})
			}
			break
		}
	}
	if inBlock {
		pieces = append(pieces, piece{blockStart, strings.Join(block, "\n")})
	}

	var out []Block
	for i := 0; i < len(pieces); {
		j := i
		text := []string{pieces[i].text}
		height := strings.Count(pieces[i].text, "\n") + 1
		for j+1 < len(pieces) && pieces[j+1].line == pieces[j].line+1 {
			j++
			text = append(text, pieces[j].text)
			height += strings.Count(pieces[j].text, "\n") + 1
		}
		out = append(out, Block{Text: strings.Join(text, "\n"), Lines: height})
		i = j + 1
	}
	return out
}

const (
	markerLine = iota
	markerBlock
)

// nextMarker finds where the next comment starts in one line of code, skipping
// string literals so a quoted "//" is not read as one.
func nextMarker(line string, st style) (int, int) {
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == st.raw && st.raw != 0 {
			if j := strings.IndexByte(line[i+1:], st.raw); j >= 0 {
				i += j + 1
				continue
			}
			return -1, 0
		}
		if isQuote(c, st.strings) {
			j := skipString(line, i, c)
			if j < 0 {
				return -1, 0
			}
			i = j
			continue
		}
		if st.blockOpen != "" && strings.HasPrefix(line[i:], st.blockOpen) {
			return i, markerBlock
		}
		for _, m := range st.line {
			if strings.HasPrefix(line[i:], m) {
				return i, markerLine
			}
		}
	}
	return -1, 0
}

func isQuote(c byte, quotes []byte) bool {
	for _, q := range quotes {
		if c == q {
			return true
		}
	}
	return false
}

// skipString returns the index of the closing quote, or -1 when the literal
// runs past the end of the line.
func skipString(line string, start int, quote byte) int {
	for i := start + 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case quote:
			return i
		}
	}
	return -1
}

// paragraphs splits a document into blank-line-separated blocks, dropping the
// parts that are not the document's own voice: fenced code, indented code, HTML
// comments and YAML frontmatter. Ported from the no-counts-in-docs sibling's
// prose walker, which draws the same boundary.
func paragraphs(doc string) []Block {
	lines := strings.Split(doc, "\n")
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}

	var out []Block
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, Block{Text: strings.Join(cur, "\n"), Lines: len(cur)})
			cur = nil
		}
	}
	inFence, inComment := false, false
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"), strings.HasPrefix(trimmed, "~~~"):
			inFence = !inFence
			flush()
			continue
		case inFence:
			continue
		case strings.Contains(line, "<!--"):
			inComment = !strings.Contains(line, "-->")
			continue
		case inComment:
			inComment = !strings.Contains(line, "-->")
			continue
		case strings.HasPrefix(line, "    "), strings.HasPrefix(line, "\t"):
			continue
		case trimmed == "":
			flush()
			continue
		}
		cur = append(cur, blankInlineCode(line))
	}
	flush()
	return out
}

// blankInlineCode replaces each backtick span with spaces, keeping every byte
// offset. A phrase inside verbatim machinery is a literal, not the document's
// own claim -- and it is how this plugin's own notes quote the shapes it
// refuses.
func blankInlineCode(line string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(line); i++ {
		if line[i] == '`' {
			in = !in
			b.WriteByte(' ')
			continue
		}
		if in {
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(line[i])
	}
	return b.String()
}
