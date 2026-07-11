package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// wantGlobSchemaCompact is an independent copy of the 2.1.116 builtin
// Glob input schema (spec section 8), compacted. The source constant must
// match it byte-for-byte.
const wantGlobSchemaCompact = `{"type":"object","additionalProperties":false,"required":["pattern"],"properties":{"pattern":{"type":"string","description":"The glob pattern to match files against"},"path":{"type":"string","description":"The directory to search in. If not specified, the current working directory will be used. IMPORTANT: Omit this field to use the default directory. DO NOT enter \"undefined\" or \"null\" - simply omit it for the default behavior. Must be a valid directory path if provided."}}}`

// wantGlobDescription is an independent copy of the verbatim 2.1.116
// builtin description (spec section 7).
const wantGlobDescription = "- Fast file pattern matching tool that works with any codebase size\n" +
	"- Supports glob patterns like \"**/*.js\" or \"src/**/*.ts\"\n" +
	"- Returns matching file paths sorted by modification time\n" +
	"- Use this tool when you need to find files by name patterns\n" +
	"- When you are doing an open ended search that may require multiple rounds of globbing and grepping, use the Agent tool instead"

func TestSchemaAndDescriptionVerbatim(t *testing.T) {
	if got := string(globInputSchemaCompact); got != wantGlobSchemaCompact {
		t.Errorf("input schema drifted from 2.1.116 spec\ngot:  %s\nwant: %s", got, wantGlobSchemaCompact)
	}
	if globDescription != wantGlobDescription {
		t.Errorf("description drifted from 2.1.116 spec\ngot:\n%q\nwant:\n%q", globDescription, wantGlobDescription)
	}
	if n := len(globDescription); n > 2048 {
		t.Errorf("description is %d chars; claude-code truncates MCP tool descriptions at 2048", n)
	}
}

func TestHandshake(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.handshake("claude-code", "2.1.207")
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: no result in %v", resp)
	}
	if got := result["protocolVersion"]; got != "2025-11-25" {
		t.Errorf("protocolVersion = %v, want echo of client's 2025-11-25", got)
	}
	si, _ := result["serverInfo"].(map[string]any)
	if si["name"] != "glob" || si["version"] == "" {
		t.Errorf("serverInfo = %v", si)
	}
	caps, _ := result["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Errorf("capabilities missing tools: %v", caps)
	}
}

func TestInitializeEchoesUnknownProtocolVersion(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01","clientInfo":{"name":"x","version":"1.0.0"}}}`)
	result := resp["result"].(map[string]any)
	if got := result["protocolVersion"]; got != "2099-01-01" {
		t.Errorf("protocolVersion = %v, want echoed 2099-01-01", got)
	}
}

func TestInitializeWithoutParams(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize without params must still succeed: %v", resp)
	}
	if got := result["protocolVersion"]; got != defaultProtocolVersion {
		t.Errorf("protocolVersion = %v, want default %s", got, defaultProtocolVersion)
	}
	// Unknown client -> tool exposed.
	if n := len(c.listTools(2)); n != 1 {
		t.Errorf("tools = %d, want 1 for unknown client", n)
	}
}

func TestToolsListEntryShape(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.recvRaw()

	// The compacted schema must appear byte-for-byte in the wire output
	// (RawMessage embeds it verbatim, preserving property order).
	if !strings.Contains(raw, wantGlobSchemaCompact) {
		t.Errorf("tools/list wire output does not contain the verbatim schema:\n%s", raw)
	}
	descJSON, _ := json.Marshal(wantGlobDescription)
	if !strings.Contains(raw, string(descJSON)) {
		t.Errorf("tools/list wire output does not contain the verbatim description:\n%s", raw)
	}
	if pi, di := strings.Index(raw, `"The glob pattern`), strings.Index(raw, `"The directory to search in`); pi < 0 || di < 0 || pi > di {
		t.Errorf("schema property order not pattern-then-path (pattern@%d path@%d)", pi, di)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "Glob" {
		t.Errorf("name = %v, want Glob", tool["name"])
	}
	ann, _ := tool["annotations"].(map[string]any)
	if ann["readOnlyHint"] != true {
		t.Errorf("annotations = %v, want readOnlyHint true", ann)
	}
	meta, _ := tool["_meta"].(map[string]any)
	if meta["anthropic/alwaysLoad"] != true {
		t.Errorf("_meta = %v, want anthropic/alwaysLoad true", meta)
	}
	schema, _ := tool["inputSchema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Errorf("inputSchema.additionalProperties = %v, want false", schema["additionalProperties"])
	}
	req, _ := schema["required"].([]any)
	if len(req) != 1 || req[0] != "pattern" {
		t.Errorf("inputSchema.required = %v, want [pattern]", req)
	}
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
			if got := len(c.listTools(2)); got != tc.wantTools {
				t.Errorf("tools = %d, want %d", got, tc.wantTools)
			}
		})
	}
}

func TestGatedOffListIsEmptyArrayAndCallsRejected(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.116")
	c.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw := c.recvRaw()
	if !strings.Contains(raw, `"tools":[]`) {
		t.Errorf("gated-off tools/list must serialize an empty array, got: %s", raw)
	}
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*"}}}`)
	if code := errorCode(t, resp); code != codeInvalidParams {
		t.Errorf("gated-off tools/call code = %d, want %d", code, codeInvalidParams)
	}
}

func TestToolsCallHappyPath(t *testing.T) {
	root := t.TempDir()
	mkFiles(t, root, "old.txt", "new.txt")
	c := startServer(t, testTool(t, root))
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*.txt"}}}`)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", resp)
	}
	if isErr, present := result["isError"]; present && isErr == true {
		t.Errorf("unexpected isError: %v", result)
	}
	content := result["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Errorf("content type = %v, want text", block["type"])
	}
	if block["text"] != "old.txt\nnew.txt" {
		t.Errorf("text = %q, want files in ascending mtime order", block["text"])
	}
}

func TestToolsCallErrorsSurfaceAsIsError(t *testing.T) {
	root := t.TempDir()
	c := startServer(t, testTool(t, root))
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Glob","arguments":{"pattern":"*","path":"missing-dir"}}}`)
	result := resp["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("want isError true, got %v", result)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	want := fmt.Sprintf("Directory does not exist: missing-dir. Note: your current working directory is %s.", root)
	if text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Nope","arguments":{}}}`)
	if code := errorCode(t, resp); code != codeInvalidParams {
		t.Errorf("code = %d, want %d", code, codeInvalidParams)
	}
}

func TestToolsCallInvalidArguments(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"missing pattern", `{}`},
		{"absent arguments", `null`},
		{"pattern wrong type", `{"pattern":42}`},
		{"path wrong type", `{"pattern":"*","path":[]}`},
		{"unexpected extra key", `{"pattern":"*","bogus":1}`},
		{"arguments not an object", `"str"`},
	}
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"Glob","arguments":%s}}`, 10+i, tc.args)
			if code := errorCode(t, c.roundTrip(req)); code != codeInvalidParams {
				t.Errorf("code = %d, want %d", code, codeInvalidParams)
			}
		})
	}
}

func TestToolsCallMissingName(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{}}`)
	if code := errorCode(t, resp); code != codeInvalidParams {
		t.Errorf("code = %d, want %d", code, codeInvalidParams)
	}
}

func TestMalformedJSONLine(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{this is not json`)
	if code := errorCode(t, resp); code != codeParseError {
		t.Errorf("code = %d, want %d", code, codeParseError)
	}
	if id, present := resp["id"]; !present || id != nil {
		t.Errorf("parse error id = %v, want null", id)
	}
	// The server must survive and keep answering.
	pong := c.roundTrip(`{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if pong["result"] == nil {
		t.Errorf("ping after parse error failed: %v", pong)
	}
}

func TestValidJSONButNotARequest(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	for _, raw := range []string{`[1,2,3]`, `"just a string"`, `42`} {
		resp := c.roundTrip(raw)
		if code := errorCode(t, resp); code != codeInvalidRequest {
			t.Errorf("%s: code = %d, want %d", raw, code, codeInvalidRequest)
		}
	}
}

func TestUnknownMethod(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)
	if code := errorCode(t, resp); code != codeMethodNotFound {
		t.Errorf("code = %d, want %d", code, codeMethodNotFound)
	}
}

func TestUnknownNotificationsTolerated(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)
	c.send(`{"jsonrpc":"2.0","method":"totally/unknown"}`)
	// No responses for either; the next request must be answered with its
	// own id, proving nothing was emitted in between.
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":77,"method":"ping"}`)
	if id, ok := resp["id"].(float64); !ok || int(id) != 77 {
		t.Errorf("id = %v, want 77", resp["id"])
	}
}

func TestPing(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	result, ok := resp["result"].(map[string]any)
	if !ok || len(result) != 0 {
		t.Errorf("ping result = %v, want {}", resp["result"])
	}
}

func TestStringAndCRLFRequestIDs(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	resp := c.roundTrip(`{"jsonrpc":"2.0","id":"abc-123","method":"ping"}` + "\r")
	if resp["id"] != "abc-123" {
		t.Errorf("id = %v, want abc-123 echoed (with CR tolerated)", resp["id"])
	}
}

func TestStdinEOFShutdown(t *testing.T) {
	c := startServer(t, testTool(t, t.TempDir()))
	c.handshake("claude-code", "2.1.207")
	c.w.Close()
	if err := <-c.done; err != nil {
		t.Errorf("run after EOF = %v, want nil", err)
	}
	// Re-arm done so Cleanup's receive does not block.
	c.done <- nil
}
