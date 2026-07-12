package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantGrepSchemaCompact is an independent copy of the amended input
// schema (spec section 4 plus the mode amendments), compacted. The
// source constant must match it byte-for-byte. Raw string: the \" and
// \u2014 sequences are JSON escapes on the wire.
const wantGrepSchemaCompact = `{"type":"object","additionalProperties":false,"required":["pattern"],"properties":{"pattern":{"type":"string","description":"The regular expression pattern to search for in file contents"},"path":{"type":"string","description":"File or directory to search in (rg PATH). Defaults to current working directory."},"glob":{"type":"string","description":"Glob pattern to filter files (e.g. \"*.js\", \"*.{ts,tsx}\") - maps to rg --glob"},"output_mode":{"type":"string","enum":["content","filenames_with_matches","filenames","count"],"description":"Output mode: \"content\" shows matching lines (supports -A/-B/-C context, -n line numbers, head_limit), \"filenames_with_matches\" shows file paths with their matching lines (supports -A/-B/-C context, -n line numbers, head_limit), \"filenames\" shows file paths (supports head_limit), \"count\" shows match counts (supports head_limit). Defaults to \"filenames_with_matches\"."},"-B":{"type":"number","description":"Number of lines to show before each match (rg -B). Requires output_mode: \"content\" or \"filenames_with_matches\", ignored otherwise."},"-A":{"type":"number","description":"Number of lines to show after each match (rg -A). Requires output_mode: \"content\" or \"filenames_with_matches\", ignored otherwise."},"-C":{"type":"number","description":"Alias for context."},"context":{"type":"number","description":"Number of lines to show before and after each match (rg -C). Requires output_mode: \"content\" or \"filenames_with_matches\", ignored otherwise."},"-n":{"type":"boolean","description":"Show line numbers in output (rg -n). Requires output_mode: \"content\" or \"filenames_with_matches\", ignored otherwise. Defaults to true."},"-i":{"type":"boolean","description":"Case insensitive search (rg -i)"},"type":{"type":"string","description":"File type to search (rg --type). Common types: js, py, rust, go, java, etc. More efficient than include for standard file types."},"head_limit":{"type":"number","description":"Limit output to first N lines/entries, equivalent to \"| head -N\". Works across all output modes: content (limits output lines), filenames_with_matches (limits match/context lines), filenames (limits file paths), count (limits count entries). Defaults to 250 when unspecified. Pass 0 for unlimited (use sparingly \u2014 large result sets waste context)."},"offset":{"type":"number","description":"Skip first N lines/entries before applying head_limit, equivalent to \"| tail -n +N | head -N\". Works across all output modes. Defaults to 0."},"multiline":{"type":"boolean","description":"Enable multiline mode where . matches newlines and patterns can span lines (rg -U --multiline-dotall). Default: false."}}}`

// wantGrepDescription is an independent copy of the description: the
// verbatim 2.1.116 builtin text (spec section 3, trailing newline
// included) with only the "Output modes" bullet rewritten.
const wantGrepDescription = "A powerful search tool built on ripgrep\n" +
	"\n" +
	"  Usage:\n" +
	"  - ALWAYS use Grep for search tasks. NEVER invoke `grep` or `rg` as a Bash command. The Grep tool has been optimized for correct permissions and access.\n" +
	"  - Supports full regex syntax (e.g., \"log.*Error\", \"function\\s+\\w+\")\n" +
	"  - Filter files with glob parameter (e.g., \"*.js\", \"**/*.tsx\") or type parameter (e.g., \"js\", \"py\", \"rust\")\n" +
	"  - Output modes: \"filenames_with_matches\" (default) groups results by file: an unindented \"path:\" header line per file (newest first), followed by that file's matching lines indented two spaces (line numbers on by default: \"N:\" for matches, \"N-\" for context lines, \"--\" between non-contiguous chunks); \"content\" shows matching lines as path:line:text; \"filenames\" shows only file paths (newest first); \"count\" shows per-file match counts\n" +
	"  - Use Agent tool for open-ended searches requiring multiple rounds\n" +
	"  - Pattern syntax: Uses ripgrep (not grep) - literal braces need escaping (use `interface\\{\\}` to find `interface{}` in Go code)\n" +
	"  - Multiline matching: By default patterns match within single lines only. For cross-line patterns like `struct \\{[\\s\\S]*?field`, use `multiline: true`\n"

func TestSchemaAndDescriptionVerbatim(t *testing.T) {
	assert.Equal(t, wantGrepSchemaCompact, string(grepInputSchemaCompact))
	assert.Equal(t, wantGrepDescription, grepDescription)
	// The description reaching the model must stay under claude-code's
	// 2048-char prompt-truncation cap.
	assert.LessOrEqual(t, len(grepDescription), 2048)
	// The amendment must not leave the retired mode name anywhere.
	assert.NotContains(t, grepDescription, "files_with_matches\"")
	assert.NotContains(t, string(grepInputSchemaCompact), `"files_with_matches"`)
}

func TestHandshake(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.handshake("claude-code", "2.1.207")
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2025-11-25", result["protocolVersion"])
	si, _ := result["serverInfo"].(map[string]any)
	assert.Equal(t, "grep", si["name"])
	assert.NotEmpty(t, si["version"])
	caps, _ := result["capabilities"].(map[string]any)
	_, ok = caps["tools"]
	assert.True(t, ok)
}

func TestInitializeEchoesUnknownProtocolVersion(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01","clientInfo":{"name":"x","version":"1.0.0"}}}`)
	result := resp["result"].(map[string]any)
	assert.Equal(t, "2099-01-01", result["protocolVersion"])
}

func TestInitializeWithoutParams(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, defaultProtocolVersion, result["protocolVersion"])
	// Unknown client -> tool exposed.
	assert.Len(t, c.listTools(2), 1)
}

func TestToolsListEntryShape(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.recvRaw()

	// The compacted schema must appear byte-for-byte in the wire output
	// (RawMessage embeds it verbatim, preserving property order).
	assert.Contains(t, raw, wantGrepSchemaCompact)

	descJSON, err := json.Marshal(wantGrepDescription)
	require.NoError(t, err)
	assert.Contains(t, raw, string(descJSON))

	// Property order on the wire: pattern before output_mode before
	// multiline.
	pi := strings.Index(raw, `"The regular expression pattern`)
	oi := strings.Index(raw, `"Output mode:`)
	mi := strings.Index(raw, `"Enable multiline mode`)
	assert.True(t, pi >= 0 && oi > pi && mi > oi, "property order drifted: %d %d %d", pi, oi, mi)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	tools := resp["result"].(map[string]any)["tools"].([]any)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]any)
	assert.Equal(t, "Grep", tool["name"])
	ann, _ := tool["annotations"].(map[string]any)
	assert.Equal(t, true, ann["readOnlyHint"])
	meta, _ := tool["_meta"].(map[string]any)
	assert.Equal(t, true, meta["anthropic/alwaysLoad"])
	schema, _ := tool["inputSchema"].(map[string]any)
	assert.Equal(t, false, schema["additionalProperties"])
	req, _ := schema["required"].([]any)
	require.Len(t, req, 1)
	assert.Equal(t, "pattern", req[0])
	// The decoded head_limit description carries a real em dash.
	props := schema["properties"].(map[string]any)
	hl := props["head_limit"].(map[string]any)
	assert.Contains(t, hl["description"], "sparingly — large")
}

func TestVersionGateMatrix(t *testing.T) {
	cases := []struct {
		name          string
		envMode       string
		clientName    string
		clientVersion string
		wantTools     int
	}{
		{"last version with builtin", "", "claude-code", "2.1.116", 0},
		{"first version without builtin", "", "claude-code", "2.1.117", 1},
		{"current version", "", "claude-code", "2.1.207", 1},
		{"older major.minor", "", "claude-code", "2.0.999", 0},
		{"newer major", "", "claude-code", "3.0.0", 1},
		{"prerelease suffix ignored", "", "claude-code", "2.1.116-beta.1", 0},
		{"other client", "", "some-other-client", "2.1.116", 1},
		{"garbage version", "", "claude-code", "garbage", 1},
		{"empty version", "", "claude-code", "", 1},
		{"two-component version", "", "claude-code", "2.1", 1},
		{"env always beats builtin-era client", "always", "claude-code", "2.1.116", 1},
		{"env never beats modern client", "never", "claude-code", "2.1.207", 0},
		{"env never beats unknown client", "never", "some-other-client", "1.0.0", 0},
		{"env mixed case", "ALWAYS", "claude-code", "2.1.116", 1},
		{"env unrecognized falls back to auto", "banana", "claude-code", "2.1.116", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(gateEnvVar, tc.envMode)
			c := startServer(t, testTool(t, t.TempDir()))
			c.handshake(tc.clientName, tc.clientVersion)
			assert.Len(t, c.listTools(2), tc.wantTools)
		})
	}
}

func TestGatedOffListIsEmptyArrayAndCallsRejected(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.116")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.recvRaw()
	assert.Contains(t, raw, `"tools":[]`)

	resp := c.roundTrip(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Grep","arguments":{"pattern":"x"}}}`)
	assert.Equal(t, codeInvalidParams, errorCode(t, resp))
}

func TestToolsCallHappyPath(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"old.txt", "needle a\n"}, tf{"new.txt", "needle b\n"})
	c := startServer(t, testTool(t, root))
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Grep","arguments":{"pattern":"needle"}}}`)
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok)
	isErr, present := result["isError"]
	assert.False(t, present && isErr == true)
	content := result["content"].([]any)
	block := content[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Equal(t, "Found 2 files\nnew.txt:\n  1:needle b\nold.txt:\n  1:needle a", block["text"])
}

func TestToolsCallErrorsSurfaceAsIsError(t *testing.T) {
	root := t.TempDir()
	c := startServer(t, testTool(t, root))
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Grep","arguments":{"pattern":"x","path":"missing-dir"}}}`)
	result := resp["result"].(map[string]any)
	require.Equal(t, true, result["isError"])
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	want := fmt.Sprintf("Path does not exist: missing-dir. Note: your current working directory is %s.", root)
	assert.Equal(t, want, text)
}

func TestToolsCallUnknownTool(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Nope","arguments":{}}}`)
	assert.Equal(t, codeInvalidParams, errorCode(t, resp))
}

func TestToolsCallMissingName(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{}}`)
	assert.Equal(t, codeInvalidParams, errorCode(t, resp))
}

func TestMalformedJSONLine(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{this is not json`)
	assert.Equal(t, codeParseError, errorCode(t, resp))
	id, present := resp["id"]
	assert.True(t, present)
	assert.Nil(t, id)

	// The server must survive and keep answering.
	pong := c.roundTrip(`{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	assert.NotNil(t, pong["result"])
}

func TestValidJSONButNotARequest(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	for _, raw := range []string{`[1,2,3]`, `"just a string"`, `42`} {
		resp := c.roundTrip(raw)
		assert.Equal(t, codeInvalidRequest, errorCode(t, resp))
	}
}

func TestUnknownMethod(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)
	assert.Equal(t, codeMethodNotFound, errorCode(t, resp))
}

func TestUnknownNotificationsTolerated(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)
	c.send(`{"jsonrpc":"2.0","method":"totally/unknown"}`)
	// No responses for either; the next request must be answered with its
	// own id, proving nothing was emitted in between.
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":77,"method":"ping"}`)
	id, ok := resp["id"].(float64)
	require.True(t, ok)
	assert.Equal(t, 77, int(id))
}

func TestPing(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, result)
}

func TestStringAndCRLFRequestIDs(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":"abc-123","method":"ping"}` + "\r")
	assert.Equal(t, "abc-123", resp["id"])
}

func TestStdinEOFShutdown(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	c.w.Close()
	assert.NoError(t, <-c.done)
	// Re-arm done so Cleanup's receive does not block.
	c.done <- nil
}
