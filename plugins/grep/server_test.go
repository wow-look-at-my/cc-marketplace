package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"rgmcp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The protocol and the gate are tested in rgmcp. These tests cover the
// grep tool as the server carries it.

func TestHandshake(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	result, ok := c.Handshake("claude-code", "2.1.207")["result"].(map[string]any)
	require.True(t, ok)
	si, _ := result["serverInfo"].(map[string]any)
	assert.Equal(t, "grep", si["name"])
}

func TestToolsListEntryShape(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.Handshake("claude-code", "2.1.207")
	c.Send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.RecvRaw()

	// The compacted schema must appear byte-for-byte in the wire output.
	assert.Contains(t, raw, string(grepInputSchemaCompact))

	descJSON, err := json.Marshal(grepDescription)
	require.NoError(t, err)
	assert.Contains(t, raw, string(descJSON))

	// The description reaching the model must stay under claude-code's 2048-char prompt-truncation cap.
	assert.LessOrEqual(t, len(grepDescription), 2048)

	// Property order on the wire: pattern before output_mode before multiline.
	pi := strings.Index(raw, `"The regular expression pattern`)
	oi := strings.Index(raw, `"Output mode.`)
	mi := strings.Index(raw, `"Patterns match single lines only`)
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
}

func TestGateEnvVarIsWired(t *testing.T) {
	assert.Equal(t, "CC_GREP_PLUGIN", gateEnvVar)
	for _, mode := range []string{"never", "always"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv(gateEnvVar, mode)
			c := startServer(t, testTool(t, t.TempDir()))
			c.Handshake("claude-code", "2.1.207")
			want := 0
			if mode == "always" {
				want = 1
			}
			assert.Len(t, c.ListTools(2), want)
		})
	}
}

func TestGatedOffListIsEmptyArrayAndCallsRejected(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.Handshake("claude-code", "2.1.116")
	c.Send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.RecvRaw()
	assert.Contains(t, raw, `"tools":[]`)

	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Grep","arguments":{"pattern":"x"}}}`)
	assert.Equal(t, rgmcp.CodeInvalidParams, errorCode(t, resp))
}

func TestToolsCallHappyPath(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, tf{"old.txt", "needle a\n"}, tf{"new.txt", "needle b\n"})
	c := startServer(t, testTool(t, root))
	c.Handshake("claude-code", "2.1.207")
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Grep","arguments":{"pattern":"needle"}}}`)
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
	c.Handshake("claude-code", "2.1.207")
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Grep","arguments":{"pattern":"x","path":"missing-dir"}}}`)
	result := resp["result"].(map[string]any)
	require.Equal(t, true, result["isError"])
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	want := fmt.Sprintf("Path does not exist: missing-dir. Note: your current working directory is %s.", root)
	assert.Equal(t, want, text)
}
