package main

import (
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestHandshake(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.handshake("claude-code", "2.1.207")
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok)

	got := result["protocolVersion"]
	assert.Equal(t, "2025-11-25", got)

	si, _ := result["serverInfo"].(map[string]any)
	assert.False(t, si["name"] != "glob" || si["version"] == "")

	caps, _ := result["capabilities"].(map[string]any)
	_, ok = caps["tools"]
	assert.True(t, ok)

}

func TestInitializeEchoesUnknownProtocolVersion(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01","clientInfo":{"name":"x","version":"1.0.0"}}}`)
	result := resp["result"].(map[string]any)
	got := result["protocolVersion"]
	assert.Equal(t, "2099-01-01", got)

}

func TestInitializeWithoutParams(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok)

	got := result["protocolVersion"]
	assert.Equal(t, defaultProtocolVersion, got)

	// Unknown client -> tool exposed.
	n := len(c.listTools(2))
	assert.Equal(t, 1, n)

}

func TestToolsListEntryShape(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.recvRaw()

	// The compacted schema must appear byte-for-byte in the wire output
	// (RawMessage embeds it verbatim, preserving property order).
	assert.Contains(t, raw, string(globInputSchemaCompact))

	descJSON, _ := json.Marshal(globDescription)
	assert.Contains(t, raw, string(descJSON))

	// The description reaching the model must stay under claude-code's
	// 2048-char prompt-truncation cap.
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
			got := len(c.listTools(2))
			assert.Equal(t, tc.wantTools, got)

		})
	}
}

func TestGatedOffListIsEmptyArrayAndCallsRejected(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.116")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.recvRaw()
	assert.Contains(t, raw, `"tools":[]`)

	resp := c.roundTrip(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*"}}}`)
	code := errorCode(t, resp)
	assert.Equal(t, codeInvalidParams, code)

}

func TestToolsCallHappyPath(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "old.txt", "new.txt")
	c := startServer(t, testTool(t, root))
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*.txt"}}}`)
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
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*","path":"missing-dir"}}}`)
	result := resp["result"].(map[string]any)
	require.Equal(t, true, result["isError"])

	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	want := fmt.Sprintf("Directory does not exist: missing-dir. Note: your current working directory is %s.", root)
	assert.Equal(t, want, text)

}

func TestToolsCallUnknownTool(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Nope","arguments":{}}}`)
	code := errorCode(t, resp)
	assert.Equal(t, codeInvalidParams, code)

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
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"Glob","arguments":%s}}`, 10+i, tc.args)
			code := errorCode(t, c.roundTrip(req))
			assert.Equal(t, codeInvalidParams, code)

		})
	}
}

func TestToolsCallMissingName(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{}}`)
	code := errorCode(t, resp)
	assert.Equal(t, codeInvalidParams, code)

}

func TestMalformedJSONLine(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{this is not json`)
	code := errorCode(t, resp)
	assert.Equal(t, codeParseError, code)

	id, present := resp["id"]
	assert.False(t, !present || id != nil)

	// The server must survive and keep answering.
	pong := c.roundTrip(`{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	assert.NotNil(t, pong["result"])

}

func TestValidJSONButNotARequest(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	for _, raw := range []string{`[1,2,3]`, `"just a string"`, `42`} {
		resp := c.roundTrip(raw)
		code := errorCode(t, resp)
		assert.Equal(t, codeInvalidRequest, code)

	}
}

func TestUnknownMethod(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)
	code := errorCode(t, resp)
	assert.Equal(t, codeMethodNotFound, code)

}

func TestUnknownNotificationsTolerated(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)
	c.send(`{"jsonrpc":"2.0","method":"totally/unknown"}`)
	// No responses for either; the next request must be answered with its
	// own id, proving nothing was emitted in between.
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":77,"method":"ping"}`)
	id, ok := resp["id"].(float64)
	assert.False(t, !ok || int(id) != 77)

}

func TestPing(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	result, ok := resp["result"].(map[string]any)
	assert.False(t, !ok || len(result) != 0)

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
