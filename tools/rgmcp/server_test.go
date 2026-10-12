package rgmcp_test

import (
	"encoding/json"
	"io"
	"testing"

	"rgmcp"
	"rgmcp/testkit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stubGateEnv = "CC_RGMCP_STUB_PLUGIN"

// stubTool echoes its "text" argument. An "error" argument makes the call fail at the JSON-RPC level.
type stubTool struct{}

func (stubTool) Name() string { return "Stub" }

func (stubTool) ListEntry() rgmcp.ToolListEntry {
	return rgmcp.ToolListEntry{
		Name:        "Stub",
		Description: "stub",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Annotations: &rgmcp.ToolAnnotations{ReadOnlyHint: true},
	}
}

func (stubTool) Call(args json.RawMessage) (*rgmcp.ToolResult, *rgmcp.RPCError) {
	var a struct {
		Text  string `json:"text"`
		Error bool   `json:"error"`
		Fail  bool   `json:"fail"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Error {
		return nil, &rgmcp.RPCError{Code: rgmcp.CodeInvalidParams, Message: "bad args"}
	}
	return &rgmcp.ToolResult{Text: a.Text, IsError: a.Fail}, nil
}

func startStub(t *testing.T) *testkit.Client {
	t.Helper()
	return testkit.Start(t, func(in io.Reader, out io.Writer) error {
		return rgmcp.NewServer(in, out, testkit.DiscardLogf, "stub", []rgmcp.Tool{stubTool{}}, stubGateEnv).Run()
	})
}

func TestHandshake(t *testing.T) {
	c := startStub(t)
	result, ok := c.Handshake("claude-code", "2.1.207")["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, rgmcp.DefaultProtocolVersion, result["protocolVersion"])
	si, _ := result["serverInfo"].(map[string]any)
	assert.Equal(t, "stub", si["name"])
	assert.NotEmpty(t, si["version"])
	caps, _ := result["capabilities"].(map[string]any)
	_, ok = caps["tools"]
	assert.True(t, ok)
}

func TestInitializeEchoesUnknownProtocolVersion(t *testing.T) {
	c := startStub(t)
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01","clientInfo":{"name":"x","version":"1.0.0"}}}`)
	result := resp["result"].(map[string]any)
	assert.Equal(t, "2099-01-01", result["protocolVersion"])
}

func TestInitializeWithoutParams(t *testing.T) {
	c := startStub(t)
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	result, ok := resp["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, rgmcp.DefaultProtocolVersion, result["protocolVersion"])
	// Unknown client -> tool exposed.
	assert.Len(t, c.ListTools(2), 1)
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
			t.Setenv(stubGateEnv, tc.envMode)
			c := startStub(t)
			c.Handshake(tc.clientName, tc.clientVersion)
			assert.Len(t, c.ListTools(2), tc.wantTools)
		})
	}
}

func TestGateEnvBeforeInitialize(t *testing.T) {
	t.Setenv(stubGateEnv, "never")
	c := startStub(t)
	assert.Empty(t, c.ListTools(1))
}

func TestGatedOffListIsEmptyArrayAndCallsRejected(t *testing.T) {
	c := startStub(t)
	c.Handshake("claude-code", "2.1.116")
	c.Send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	assert.Contains(t, c.RecvRaw(), `"tools":[]`)
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Stub","arguments":{"text":"x"}}}`)
	assert.Equal(t, rgmcp.CodeInvalidParams, testkit.ErrorCode(t, resp))
}

func TestToolsListEntryShape(t *testing.T) {
	c := startStub(t)
	c.Handshake("claude-code", "2.1.207")
	c.Send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.RecvRaw()
	assert.Contains(t, raw, `"inputSchema":{"type":"object"}`)
	assert.Contains(t, raw, `"annotations":{"readOnlyHint":true}`)
	assert.NotContains(t, raw, `"_meta"`)
}

func TestToolsCallResultShape(t *testing.T) {
	c := startStub(t)
	c.Handshake("claude-code", "2.1.207")
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Stub","arguments":{"text":"hello"}}}`)
	result := resp["result"].(map[string]any)
	_, present := result["isError"]
	assert.False(t, present)
	block := result["content"].([]any)[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Equal(t, "hello", block["text"])

	resp = c.RoundTrip(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Stub","arguments":{"text":"oops","fail":true}}}`)
	assert.Equal(t, true, resp["result"].(map[string]any)["isError"])
}

func TestToolsCallRPCErrorPassesThrough(t *testing.T) {
	c := startStub(t)
	c.Handshake("claude-code", "2.1.207")
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Stub","arguments":{"error":true}}}`)
	assert.Equal(t, rgmcp.CodeInvalidParams, testkit.ErrorCode(t, resp))
	assert.Equal(t, "bad args", resp["error"].(map[string]any)["message"])
}

func TestToolsCallUnknownTool(t *testing.T) {
	c := startStub(t)
	c.Handshake("claude-code", "2.1.207")
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Nope","arguments":{}}}`)
	assert.Equal(t, rgmcp.CodeInvalidParams, testkit.ErrorCode(t, resp))
}

func TestToolsCallMissingName(t *testing.T) {
	c := startStub(t)
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{}}`)
	assert.Equal(t, rgmcp.CodeInvalidParams, testkit.ErrorCode(t, resp))
}

func TestMalformedJSONLine(t *testing.T) {
	c := startStub(t)
	resp := c.RoundTrip(`{this is not json`)
	assert.Equal(t, rgmcp.CodeParseError, testkit.ErrorCode(t, resp))
	id, present := resp["id"]
	assert.True(t, present)
	assert.Nil(t, id)

	// The server must survive and keep answering.
	pong := c.RoundTrip(`{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	assert.NotNil(t, pong["result"])
}

func TestValidJSONButNotARequest(t *testing.T) {
	c := startStub(t)
	for _, raw := range []string{`[1,2,3]`, `"just a string"`, `42`} {
		assert.Equal(t, rgmcp.CodeInvalidRequest, testkit.ErrorCode(t, c.RoundTrip(raw)))
	}
}

func TestUnknownMethod(t *testing.T) {
	c := startStub(t)
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)
	assert.Equal(t, rgmcp.CodeMethodNotFound, testkit.ErrorCode(t, resp))
}

func TestUnknownNotificationsTolerated(t *testing.T) {
	c := startStub(t)
	c.Send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)
	c.Send(`{"jsonrpc":"2.0","method":"totally/unknown"}`)
	// No responses for either; the next request must be answered with its own id, proving nothing was emitted in between.
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":77,"method":"ping"}`)
	id, ok := resp["id"].(float64)
	require.True(t, ok)
	assert.Equal(t, 77, int(id))
}

func TestPing(t *testing.T) {
	c := startStub(t)
	result, ok := c.RoundTrip(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)["result"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, result)
}

func TestStringAndCRLFRequestIDs(t *testing.T) {
	c := startStub(t)
	resp := c.RoundTrip(`{"jsonrpc":"2.0","id":"abc-123","method":"ping"}` + "\r")
	assert.Equal(t, "abc-123", resp["id"])
}

func TestStdinEOFShutdown(t *testing.T) {
	c := startStub(t)
	c.Handshake("claude-code", "2.1.207")
	assert.NoError(t, c.Shutdown())
}
