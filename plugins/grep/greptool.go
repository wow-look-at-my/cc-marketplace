// greptool.go implements the Grep tool: description, input schema,
// argument parsing (with the builtin's zod coercions), ripgrep argv
// construction, and path validation. Behavior mirrors the builtin
// removed from claude-code (version 2.1.116, the last release that
// registered it by default) except for the redesigned output-mode set:
// the ambiguous files_with_matches default was replaced by
// "filenames_with_matches" (paths grouped with their matching lines) and
// "filenames" (the old name-only listing). See grepmodes.go/grepfwm.go
// for the per-mode rendering.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const grepToolName = "Grep"

// Output mode names. The builtin's enum was
// [content, files_with_matches, count] with files_with_matches (a bare
// newest-first path list) as the default; this plugin deliberately drops
// that name (no alias) and ships the amended set below.
const (
	modeContent              = "content"
	modeFilenamesWithMatches = "filenames_with_matches"
	modeFilenames            = "filenames"
	modeCount                = "count"
)

// grepDescription is a streamlined rewrite of the 2.1.116 builtin
// description (2.1.116:cli.js:113993-114005). Parameters are documented
// in the schema, not here. The builtin's brace-escaping caveat is gone:
// it existed because the builtin swallowed rg parse errors as "No
// matches found", whereas this plugin surfaces them, so a bad pattern
// explains itself. The tool is alwaysLoad, so every description byte is
// paid in every prompt.
const grepDescription = "A search tool built on ripgrep; patterns use ripgrep's full regex syntax.\n" +
	"ALWAYS use Grep for search tasks. NEVER invoke `grep` or `rg` as a Bash command.\n"

// schemaProp is one JSON Schema property entry.
type schemaProp struct {
	Type        string   `json:"type"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description"`
}

// grepSchema is the builtin input schema shape (zod strictObject ->
// additionalProperties false, required: pattern;
// 2.1.116:cli.js:286206-286229) with the amended output-mode enum and
// condensed property descriptions. The output modes and their formats
// are documented solely on output_mode. Struct field order is the
// property order the model sees.
type grepSchema struct {
	Type                 string          `json:"type"`
	AdditionalProperties bool            `json:"additionalProperties"`
	Required             []string        `json:"required"`
	Properties           grepSchemaProps `json:"properties"`
}

type grepSchemaProps struct {
	Pattern    schemaProp `json:"pattern"`
	Path       schemaProp `json:"path"`
	Glob       schemaProp `json:"glob"`
	OutputMode schemaProp `json:"output_mode"`
	DashB      schemaProp `json:"-B"`
	DashA      schemaProp `json:"-A"`
	DashC      schemaProp `json:"-C"`
	Context    schemaProp `json:"context"`
	DashN      schemaProp `json:"-n"`
	DashI      schemaProp `json:"-i"`
	HeadLimit  schemaProp `json:"head_limit"`
	Offset     schemaProp `json:"offset"`
	Multiline  schemaProp `json:"multiline"`
}

var grepInputSchemaCompact = mustMarshalJSON(grepSchema{
	Type:     "object",
	Required: []string{"pattern"},
	Properties: grepSchemaProps{
		Pattern: schemaProp{Type: "string", Description: "The regular expression pattern to search for in file contents"},
		Path:    schemaProp{Type: "string", Description: "File or directory to search in (rg PATH). Defaults to current working directory."},
		Glob:    schemaProp{Type: "string", Description: "Glob pattern to filter files (e.g. \"*.js\", \"*.{ts,tsx}\") - maps to rg --glob"},
		OutputMode: schemaProp{
			Type:        "string",
			Enum:        []string{modeContent, modeFilenamesWithMatches, modeFilenames, modeCount},
			Description: "Output mode. \"filenames_with_matches\" (default): results grouped by file - an unindented \"path:\" header per file (newest first), that file's matching lines indented beneath (\"N:\" match, \"N-\" context, \"--\" between non-contiguous chunks); \"content\": matching lines as path:line:text; \"filenames\": file paths only (newest first); \"count\": per-file match counts.",
		},
		DashB:     schemaProp{Type: "number", Description: "Lines to show before each match (rg -B). content and filenames_with_matches modes only."},
		DashA:     schemaProp{Type: "number", Description: "Lines to show after each match (rg -A). content and filenames_with_matches modes only."},
		DashC:     schemaProp{Type: "number", Description: "Alias for context."},
		Context:   schemaProp{Type: "number", Description: "Lines to show before and after each match (rg -C). content and filenames_with_matches modes only."},
		DashN:     schemaProp{Type: "boolean", Description: "Show line numbers (rg -n). content and filenames_with_matches modes only. Defaults to true."},
		DashI:     schemaProp{Type: "boolean", Description: "Case insensitive search (rg -i)"},
		HeadLimit: schemaProp{Type: "number", Description: "Limit output to the first N lines/entries. Defaults to 250; 0 = unlimited. Applies to all output modes."},
		Offset:    schemaProp{Type: "number", Description: "Skip the first N lines/entries before applying head_limit. Defaults to 0."},
		Multiline: schemaProp{Type: "boolean", Description: "Patterns match single lines only unless this is set; then . matches newlines and patterns can span lines (rg -U --multiline-dotall). Default: false."},
	},
})

func mustMarshalJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

const (
	// grepPersistThreshold = min(maxResultSizeChars 20000, persistence
	// ceiling 50000) (2.1.116:cli.js:286237, 155809).
	grepPersistThreshold = 20000
	// defaultHeadLimit mirrors H61 = 250 (2.1.116:cli.js:286189).
	defaultHeadLimit = 250
)

// vcsExclusions mirrors e81 (2.1.116:cli.js:286230).
var vcsExclusions = []string{".git", ".svn", ".hg", ".bzr", ".jj", ".sl"}

// numericStringRe is the builtin's zod-preprocess coercion pattern for
// number params (tE, 2.1.116:cli.js:286087-286094).
var numericStringRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

var grepOutputModes = []string{modeContent, modeFilenamesWithMatches, modeFilenames, modeCount}

type grepArgs struct {
	pattern    string
	path       string
	globPat    string
	mode       string
	before     *float64 // -B
	after      *float64 // -A
	dashC      *float64 // -C
	context    *float64
	lineNums   bool     // -n, default true
	ignoreCase bool     // -i
	headLimit  *float64 // nil = default 250; 0 = unlimited
	offset     float64
	multiline  bool
}

type grepTool struct {
	root             string // default search root (session-cwd equivalent)
	persistThreshold int
	timeout          time.Duration
	timeoutLabel     int
	maxOutput        int
	tempDir          string // persist dir; "" = os.TempDir()
	resolveRg        func() (string, error)
	logf             func(string, ...any)
}

// newGrepTool builds the production tool: root = $CLAUDE_PROJECT_DIR
// (injected into every plugin MCP server by claude-code) falling back to
// the process cwd.
func newGrepTool(logf func(string, ...any)) *grepTool {
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		} else {
			root = "."
		}
	}
	timeout, label := defaultRgTimeout()
	return &grepTool{
		root:             root,
		persistThreshold: grepPersistThreshold,
		timeout:          timeout,
		timeoutLabel:     label,
		maxOutput:        rgOutputCapBytes,
		resolveRg:        resolveRipgrep,
		logf:             logf,
	}
}

func (g *grepTool) Name() string { return grepToolName }

func (g *grepTool) ListEntry() toolListEntry {
	return toolListEntry{
		Name:        grepToolName,
		Description: grepDescription,
		InputSchema: grepInputSchemaCompact,
		Annotations: &toolAnnotations{ReadOnlyHint: true},
		Meta:        map[string]any{"anthropic/alwaysLoad": true},
	}
}

// Call validates the arguments against the schema (JSON-RPC-level
// failures) and executes the search (operational failures become
// isError results).
func (g *grepTool) Call(raw json.RawMessage) (*toolResult, *rpcError) {
	args, rpcErr := parseGrepArgs(raw)
	if rpcErr != nil {
		return nil, rpcErr
	}
	text, isErr := g.execute(args)
	return &toolResult{Text: text, IsError: isErr}, nil
}

func parseGrepArgs(raw json.RawMessage) (*grepArgs, *rpcError) {
	invalid := func(format string, fa ...any) (*grepArgs, *rpcError) {
		return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf(format, fa...)}
	}
	var m map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return invalid("%s arguments must be an object", grepToolName)
		}
	}
	// Defaults per the builtin destructuring (2.1.116:cli.js:286337).
	a := &grepArgs{mode: modeFilenamesWithMatches, lineNums: true}
	seenPattern := false
	for k, v := range m {
		switch k {
		case "pattern", "path", "glob", "output_mode":
			var s string
			// Explicit null check: Unmarshal treats JSON null as a no-op
			// on Go scalars, but zod rejects null for these fields.
			if isJSONNull(v) || json.Unmarshal(v, &s) != nil {
				return invalid("%s %s must be a string", grepToolName, k)
			}
			switch k {
			case "pattern":
				a.pattern, seenPattern = s, true
			case "path":
				a.path = s
			case "glob":
				a.globPat = s
			default: // output_mode
				if !slices.Contains(grepOutputModes, s) {
					return invalid(`%s output_mode must be one of "content", "filenames_with_matches", "filenames", "count"`, grepToolName)
				}
				a.mode = s
			}
		case "-B", "-A", "-C", "context", "head_limit", "offset":
			n, ok := coerceNumber(v)
			if !ok {
				return invalid("%s %s must be a number", grepToolName, k)
			}
			switch k {
			case "-B":
				a.before = &n
			case "-A":
				a.after = &n
			case "-C":
				a.dashC = &n
			case "context":
				a.context = &n
			case "head_limit":
				a.headLimit = &n
			default: // offset
				a.offset = n
			}
		case "-n", "-i", "multiline":
			b, ok := coerceBool(v)
			if !ok {
				return invalid("%s %s must be a boolean", grepToolName, k)
			}
			switch k {
			case "-n":
				a.lineNums = b
			case "-i":
				a.ignoreCase = b
			default: // multiline
				a.multiline = b
			}
		default:
			// additionalProperties: false
			return invalid("%s does not accept an argument named %q", grepToolName, k)
		}
	}
	if !seenPattern {
		return invalid("%s requires the pattern argument", grepToolName)
	}
	return a, nil
}

func isJSONNull(v json.RawMessage) bool {
	return string(bytes.TrimSpace(v)) == "null"
}

// coerceNumber accepts a JSON number, or (mirroring zod preprocess tE,
// 2.1.116:cli.js:286087-286094) a string matching /^-?\d+(\.\d+)?$/.
func coerceNumber(v json.RawMessage) (float64, bool) {
	if isJSONNull(v) {
		return 0, false
	}
	var f float64
	if json.Unmarshal(v, &f) == nil {
		return f, true
	}
	var s string
	if json.Unmarshal(v, &s) == nil && numericStringRe.MatchString(s) {
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// coerceBool accepts a JSON boolean, or the exact strings "true"/"false"
// (zod preprocess cL, 2.1.116:cli.js:284285-284287).
func coerceBool(v json.RawMessage) (bool, bool) {
	if isJSONNull(v) {
		return false, false
	}
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return b, true
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		switch s {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// execute runs one Grep search, returning the tool_result text and
// whether it is an error.
func (g *grepTool) execute(a *grepArgs) (string, bool) {
	searchPath := g.root
	if a.path != "" { // empty string is falsy upstream: same as omitted
		resolved, err := resolveAgainst(a.path, g.root)
		if err != nil {
			return err.Error(), true
		}
		if msg, ok := g.validatePath(a.path, resolved); !ok {
			return msg, true
		}
		searchPath = resolved
	}

	rgPath, err := g.resolveRg()
	if err != nil {
		return err.Error(), true
	}

	args := append(buildRgArgs(a), searchPath)
	runner := &rgRunner{timeout: g.timeout, timeoutLabel: g.timeoutLabel, maxOutput: g.maxOutput}
	lines, err := runner.run(rgPath, args, g.root)
	if err != nil {
		return err.Error(), true
	}

	var text string
	switch a.mode {
	case modeContent:
		text = g.formatContent(lines, a)
	case modeCount:
		text = g.formatCount(lines, a)
	case modeFilenames:
		text = g.formatFilenames(lines, a)
	default:
		text = g.formatFilenamesWithMatches(lines, a)
	}
	return persistOversize(text, grepToolName, g.persistThreshold, g.tempDir, g.logf), false
}

// buildRgArgs constructs the rg argv in the builtin's exact order
// (2.1.116:cli.js:286337-286368) with four amendments: the builtin's
// --max-columns 500 is dropped (long lines are shown, then clamped in Go
// per clamp.go, instead of omitted by rg), the mode-flag slot emits --json
// for filenames_with_matches (rendered in Go from rg's JSON events), count
// mode gains -H (claude-code's own >=2.1.175 fix for single-file count
// parsing), and the context flags apply to filenames_with_matches as well
// as content. The permission deny-rule and claude-internal cache
// exclusions the builtin appended are not available to a plugin and are
// omitted.
func buildRgArgs(a *grepArgs) []string {
	args := []string{"--hidden"}
	for _, d := range vcsExclusions {
		args = append(args, "--glob", "!"+d)
	}
	// No --max-columns: the builtin capped rg at 500 columns and omitted
	// longer lines; per decree they are shown, then clamped in Go (clamp.go).
	// filenames_with_matches always relied on full lines (rg --json ignores it).
	if a.multiline {
		args = append(args, "-U", "--multiline-dotall")
	}
	if a.ignoreCase {
		args = append(args, "-i")
	}
	switch a.mode {
	case modeFilenames:
		args = append(args, "-l")
	case modeCount:
		args = append(args, "-c", "-H")
	case modeFilenamesWithMatches:
		args = append(args, "--json")
	}
	if a.lineNums && a.mode == modeContent {
		args = append(args, "-n")
	}
	if a.mode == modeContent || a.mode == modeFilenamesWithMatches {
		switch {
		case a.context != nil:
			args = append(args, "-C", jsNumString(*a.context))
		case a.dashC != nil:
			args = append(args, "-C", jsNumString(*a.dashC))
		default:
			if a.before != nil {
				args = append(args, "-B", jsNumString(*a.before))
			}
			if a.after != nil {
				args = append(args, "-A", jsNumString(*a.after))
			}
		}
	}
	if strings.HasPrefix(a.pattern, "-") {
		args = append(args, "-e", a.pattern)
	} else {
		args = append(args, a.pattern)
	}
	for _, gl := range tokenizeGlobParam(a.globPat) {
		args = append(args, "--glob", gl)
	}
	return args
}

// tokenizeGlobParam mirrors the builtin's glob splitting
// (2.1.116:cli.js:286356-286363): whitespace split; tokens containing
// both "{" and "}" stay whole; other tokens are comma-split; empties
// dropped.
func tokenizeGlobParam(s string) []string {
	var out []string
	for _, tok := range strings.Fields(s) {
		if strings.Contains(tok, "{") && strings.Contains(tok, "}") {
			out = append(out, tok)
			continue
		}
		for _, part := range strings.Split(tok, ",") {
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// validatePath mirrors the builtin Grep validateInput
// (2.1.116:cli.js:286270-286293): UNC-ish resolved paths skip
// validation, ENOENT yields the path-does-not-exist message (with a
// did-you-mean suggestion), and other stat errors propagate raw. Unlike
// Glob there is no isDirectory check: file paths are accepted.
func (g *grepTool) validatePath(rawPath, resolved string) (string, bool) {
	if strings.HasPrefix(resolved, `\\`) || strings.HasPrefix(resolved, "//") {
		return "", true
	}
	if _, err := os.Stat(resolved); err != nil {
		if os.IsNotExist(err) {
			msg := fmt.Sprintf("Path does not exist: %s. %s %s.", rawPath, cwdNote, g.root)
			if s := didYouMean(resolved, g.root); s != "" {
				msg += fmt.Sprintf(" Did you mean %s?", s)
			}
			return msg, false
		}
		return err.Error(), false
	}
	return "", true
}
