// globtool.go implements the Glob tool with the exact behavior of the
// builtin removed from claude-code: description, input schema, ripgrep
// argv, sorting, truncation, and result formatting all mirror version
// 2.1.116 (the last release that registered the builtin by default).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const globToolName = "Glob"

// globDescription is the verbatim builtin description
// (2.1.116:cli.js:114088-114093; no trailing newline, well under the
// 2048-char cap claude-code applies to MCP tool descriptions).
const globDescription = `- Fast file pattern matching tool that works with any codebase size
- Supports glob patterns like "**/*.js" or "src/**/*.ts"
- Returns matching file paths sorted by modification time
- Use this tool when you need to find files by name patterns
- When you are doing an open ended search that may require multiple rounds of globbing and grepping, use the Agent tool instead`

// globInputSchemaJSON is the verbatim builtin input schema
// (2.1.116:cli.js:286491-286496; zod strictObject -> additionalProperties
// false, required: pattern). Kept as raw JSON so the property order and
// description strings reach the model byte-for-byte.
const globInputSchemaJSON = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["pattern"],
  "properties": {
    "pattern": {
      "type": "string",
      "description": "The glob pattern to match files against"
    },
    "path": {
      "type": "string",
      "description": "The directory to search in. If not specified, the current working directory will be used. IMPORTANT: Omit this field to use the default directory. DO NOT enter \"undefined\" or \"null\" - simply omit it for the default behavior. Must be a valid directory path if provided."
    }
  }
}`

var globInputSchemaCompact = mustCompactJSON(globInputSchemaJSON)

func mustCompactJSON(s string) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		panic(err)
	}
	return json.RawMessage(buf.Bytes())
}

const (
	// globMaxResults is the effective 2.1.116 limit: the executor passes
	// globLimits.maxResults = 25000 (2.1.116:cli.js:304000). The "limited
	// to 100 files" text in the old internal OUTPUT schema was stale.
	globMaxResults = 25000
	// globPersistThreshold = min(maxResultSizeChars 100000, ceiling
	// 50000) (2.1.116:cli.js:286508, 155809).
	globPersistThreshold = 50000
	globTruncationLine   = "(Results are truncated. Consider using a more specific path or pattern.)"
	globNoFilesFound     = "No files found"
	cwdNote              = "Note: your current working directory is"
)

type globTool struct {
	root             string // default search root (session-cwd equivalent)
	maxResults       int
	persistThreshold int
	timeout          time.Duration
	timeoutLabel     int
	maxOutput        int
	tempDir          string // persist dir; "" = os.TempDir()
	resolveRg        func() (string, error)
	logf             func(string, ...any)
}

// newGlobTool builds the production tool: root = $CLAUDE_PROJECT_DIR
// (injected into every plugin MCP server by claude-code) falling back to
// the process cwd.
func newGlobTool(logf func(string, ...any)) *globTool {
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		} else {
			root = "."
		}
	}
	timeout, label := defaultRgTimeout()
	return &globTool{
		root:             root,
		maxResults:       globMaxResults,
		persistThreshold: globPersistThreshold,
		timeout:          timeout,
		timeoutLabel:     label,
		maxOutput:        rgOutputCapBytes,
		resolveRg:        resolveRipgrep,
		logf:             logf,
	}
}

func (g *globTool) Name() string { return globToolName }

func (g *globTool) ListEntry() toolListEntry {
	return toolListEntry{
		Name:        globToolName,
		Description: globDescription,
		InputSchema: globInputSchemaCompact,
		Annotations: &toolAnnotations{ReadOnlyHint: true},
		Meta:        map[string]any{"anthropic/alwaysLoad": true},
	}
}

// Call validates the arguments against the schema (JSON-RPC-level
// failures) and executes the search (operational failures become
// isError results).
func (g *globTool) Call(raw json.RawMessage) (*toolResult, *rpcError) {
	pattern, path, rpcErr := parseGlobArgs(raw)
	if rpcErr != nil {
		return nil, rpcErr
	}
	text, isErr := g.execute(pattern, path)
	return &toolResult{Text: text, IsError: isErr}, nil
}

func parseGlobArgs(raw json.RawMessage) (pattern, path string, rpcErr *rpcError) {
	invalid := func(format string, args ...any) (string, string, *rpcError) {
		return "", "", &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf(format, args...)}
	}
	var m map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return invalid("%s arguments must be an object", globToolName)
		}
	}
	seenPattern := false
	for k, v := range m {
		switch k {
		case "pattern":
			if json.Unmarshal(v, &pattern) != nil {
				return invalid("%s pattern must be a string", globToolName)
			}
			seenPattern = true
		case "path":
			if json.Unmarshal(v, &path) != nil {
				return invalid("%s path must be a string", globToolName)
			}
		default:
			// additionalProperties: false
			return invalid("%s does not accept an argument named %q", globToolName, k)
		}
	}
	if !seenPattern {
		return invalid("%s requires the pattern argument", globToolName)
	}
	return pattern, path, nil
}

// execute runs one Glob search, returning the tool_result text and
// whether it is an error.
func (g *globTool) execute(pattern, path string) (string, bool) {
	searchPath := g.root
	if path != "" { // empty string is falsy upstream: same as omitted
		resolved := resolveAgainst(path, g.root)
		if msg, ok := g.validateDir(path, resolved); !ok {
			return msg, true
		}
		searchPath = resolved
	}

	// An absolute pattern overrides the search root (a81,
	// 2.1.116:cli.js:286040-286056).
	pat := pattern
	if filepath.IsAbs(pat) {
		if base, rel := splitAbsolutePattern(pat); base != "" {
			searchPath, pat = base, rel
		}
	}

	rgPath, err := g.resolveRg()
	if err != nil {
		return err.Error(), true
	}

	// argv per N57 (2.1.116:cli.js:286057-286078). The gitignore/hidden
	// defaults are env-overridable exactly like the builtin.
	args := []string{"--files", "--glob", pat, "--sort=modified"}
	if envTruthyDefault("CLAUDE_CODE_GLOB_NO_IGNORE", "true") {
		args = append(args, "--no-ignore")
	}
	if envTruthyDefault("CLAUDE_CODE_GLOB_HIDDEN", "true") {
		args = append(args, "--hidden")
	}
	args = append(args, searchPath)

	runner := &rgRunner{timeout: g.timeout, timeoutLabel: g.timeoutLabel, maxOutput: g.maxOutput}
	lines, err := runner.run(rgPath, args, g.root)
	if err != nil {
		return err.Error(), true
	}

	files := make([]string, 0, len(lines))
	for _, l := range lines {
		if !filepath.IsAbs(l) {
			l = filepath.Join(searchPath, l)
		}
		files = append(files, l)
	}
	truncated := len(files) > g.maxResults
	if truncated {
		files = files[:g.maxResults]
	}
	for i, f := range files {
		files[i] = relativizePath(f, g.root)
	}

	var text string
	if len(files) == 0 {
		text = globNoFilesFound
	} else {
		text = strings.Join(files, "\n")
		if truncated {
			text += "\n" + globTruncationLine
		}
	}
	return persistOversize(text, globToolName, g.persistThreshold, g.tempDir, g.logf), false
}

// validateDir mirrors the builtin validateInput
// (2.1.116:cli.js:286538-286561): UNC-ish resolved paths skip validation,
// ENOENT yields the directory-does-not-exist message (with a did-you-mean
// suggestion), other stat errors propagate raw, and non-directories yield
// the not-a-directory message. Messages interpolate the RAW path argument
// and the default root.
func (g *globTool) validateDir(rawPath, resolved string) (string, bool) {
	if strings.HasPrefix(resolved, `\\`) || strings.HasPrefix(resolved, "//") {
		return "", true
	}
	st, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			msg := fmt.Sprintf("Directory does not exist: %s. %s %s.", rawPath, cwdNote, g.root)
			if s := didYouMean(resolved, g.root); s != "" {
				msg += fmt.Sprintf(" Did you mean %s?", s)
			}
			return msg, false
		}
		return err.Error(), false
	}
	if !st.IsDir() {
		return fmt.Sprintf("Path is not a directory: %s", rawPath), false
	}
	return "", true
}

// resolveAgainst resolves p against root (absolute paths pass through
// cleaned), like Node path.resolve(root, p). Divergence: no unicode NFC
// normalization (the builtin NFC-normalizes; stdlib-only here).
func resolveAgainst(p, root string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(root, p)
}

// splitAbsolutePattern mirrors a81: split an absolute pattern at the
// first glob metachar [*?[{]; the base dir is everything before the last
// separator preceding it and the rest is the relative pattern. Without a
// metachar the split is dirname/basename. (The Windows drive-letter
// special case is omitted: this plugin ships linux/darwin binaries only.)
func splitAbsolutePattern(pat string) (base, rel string) {
	idx := strings.IndexAny(pat, "*?[{")
	if idx < 0 {
		return filepath.Dir(pat), filepath.Base(pat)
	}
	slash := strings.LastIndex(pat[:idx], "/")
	if slash < 0 {
		return "", pat
	}
	base = pat[:slash]
	if base == "" {
		base = "/" // metachar in the first component after the root slash
	}
	return base, pat[slash+1:]
}

// relativizePath mirrors QZH (2.1.116:cli.js:35616-35619): root-relative
// when under root, absolute otherwise (including the faithful quirk that
// any relative form starting with ".." — even a "..foo" sibling name —
// falls back to absolute).
func relativizePath(abs, root string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

// didYouMean ports Vde (2.1.207:cli.js:44437-44455): when the missing
// path resolved into the parent of root but outside root, re-root it
// under root and suggest that absolute path if it exists.
func didYouMean(resolved, root string) string {
	sep := string(filepath.Separator)
	parent := filepath.Dir(root)
	n := resolved
	if rp, err := filepath.EvalSymlinks(filepath.Dir(resolved)); err == nil {
		n = filepath.Join(rp, filepath.Base(resolved))
	}
	prefix := parent
	if parent != sep {
		prefix = parent + sep
	}
	if !strings.HasPrefix(n, prefix) || strings.HasPrefix(n, root+sep) || n == root {
		return ""
	}
	rel, err := filepath.Rel(parent, n)
	if err != nil {
		return ""
	}
	candidate := filepath.Join(root, rel)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// envTruthyDefault mirrors yH(process.env.X || fallback)
// (2.1.116:cli.js:1666-1671): the value (or fallback when unset/empty)
// counts as truthy only when it is one of 1/true/yes/on,
// case-insensitively.
func envTruthyDefault(name, fallback string) bool {
	v := os.Getenv(name)
	if v == "" {
		v = fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
