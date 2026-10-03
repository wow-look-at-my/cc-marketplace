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
// glob tool as the server carries it.

func TestHandshake(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	result, ok := c.Handshake("claude-code", "2.1.207")["result"].(map[string]any)
	require.True(t, ok)
	si, _ := result["serverInfo"].(map[string]any)
	assert.Equal(t, "glob", si["name"])
}

func TestToolsListEntryShape(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.Handshake("claude-code", "2.1.207")
	c.Send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.RecvRaw()

	// The compacted schema must appear byte-for-byte in the wire output.
	assert.Contains(t, raw, string(globInputSchemaCompact))

	descJSON, _ := json.Marshal(globDescription)
	assert.Contains(t, raw, string(descJSON))

	// The description reaching the model must stay under claude-code's 2048-char prompt-truncation cap.
	assert.LessOrEqual(t, len(globDescription), 2048)

	pi, di := strings.Index(raw, `"The glob pattern`), strings.Index(raw, `"The directory to search in`)
	assert.False(t, pi < 0 || di < 0 || pi > di)

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))

	tools := resp["result"].(map[string]any)["tools"].([]any)
	require.Equal(t, 1, len(tools))

	tool := tools[0].(map[string]any)
	assert.Equal(t, "Glob", tool["name"])

	ann, _ := tool["annotations"].(map[string]any)
	assert.Equal(t, true, ann["readOnlyHint"])

	meta, _ := tool["_meta"].(map[string]any)
	assert.Equal(t, true, meta["anthropic/alwaysLoad"])

	schema, _ := tool["inputSchema"].(map[string]any)
	assert.Equal(t, false, schema["additionalProperties"])

	req, _ := schema["required"].([]any)
	assert.False(t, len(req) != 1 || req[0] != "pattern")

}

func TestGateEnvVarIsWired(t *testing.T) {
	assert.Equal(t, "CC_GLOB_PLUGIN", gateEnvVar)
	cases := []struct {
		mode, version string
		wantTools     int
	}{
		{"", "2.1.116", 0},
		{"", "2.1.207", 1},
		{"always", "2.1.116", 1},
		{"never", "2.1.207", 0},
	}
	for _, tc := range cases {
		t.Run(tc.mode+"/"+tc.version, func(t *testing.T) {
			t.Setenv(gateEnvVar, tc.mode)
			c := startServer(t, testTool(t, t.TempDir()))
			c.Handshake("claude-code", tc.version)
			assert.Len(t, c.ListTools(2), tc.wantTools)
		})
	}
}

func TestGatedOffListIsEmptyArrayAndCallsRejected(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.Handshake("claude-code", "2.1.116")
	c.Send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.RecvRaw()
	assert.Contains(t, raw, `"tools":[]`)

	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*"}}}`)
	code := errorCode(t, resp)
	assert.Equal(t, rgmcp.CodeInvalidParams, code)

}

func TestToolsCallHappyPath(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "old.txt", "new.txt")
	c := startServer(t, testTool(t, root))
	c.Handshake("claude-code", "2.1.207")
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*.txt"}}}`)
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok)

	isErr, present := result["isError"]
	assert.False(t, present && isErr == true)

	content := result["content"].([]any)
	block := content[0].(map[string]any)
	assert.Equal(t, "text", block["type"])

	assert.Equal(t, "old.txt\nnew.txt", block["text"])

}

func TestToolsCallErrorsSurfaceAsIsError(t *testing.T) {
	root := t.TempDir()
	c := startServer(t, testTool(t, root))
	c.Handshake("claude-code", "2.1.207")
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*","path":"missing-dir"}}}`)
	result := resp["result"].(map[string]any)
	require.Equal(t, true, result["isError"])

	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	want := fmt.Sprintf("Directory does not exist: missing-dir. Note: your current working directory is %s.", root)
	assert.Equal(t, want, text)

}

func TestToolsCallInvalidArguments(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"missing pattern", `{}`},
		{"absent arguments", `null`},
		{"pattern wrong type", `{"pattern":42}`},
		{"pattern null", `{"pattern":null}`}, // would otherwise list the whole tree
		{"path wrong type", `{"pattern":"*","path":[]}`},
		{"path null", `{"pattern":"*","path":null}`},
		{"unexpected extra key", `{"pattern":"*","bogus":1}`},
		{"arguments not an object", `"str"`},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A single server per subtest: a pipeClient carries a request and its response on a single pipe.
			c := startServer(t, testTool(t, t.TempDir()))
			c.Handshake("claude-code", "2.1.207")
			req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"Glob","arguments":%s}}`, 10+i, tc.args)
			code := errorCode(t, c.RoundTrip(req))
			assert.Equal(t, rgmcp.CodeInvalidParams, code)

		})
	}
}
