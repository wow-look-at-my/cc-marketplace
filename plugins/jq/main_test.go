package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name string `json:"name"`
	} `json:"clientInfo"`
	Capabilities struct{} `json:"capabilities"`
}

type initializeResult struct {
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

type toolsListResult struct {
	Tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type callToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func startServer(t *testing.T) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
	t.Helper()

	cmd := exec.Command("./run")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("failed to get stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to get stdout: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	return cmd, stdin, bufio.NewReader(stdout)
}

func sendRequest(t *testing.T, stdin io.Writer, stdout *bufio.Reader, req jsonRPCRequest) jsonRPCResponse {
	t.Helper()

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	_, err = stdin.Write(append(data, '\n'))
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	line, err := stdout.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("failed to parse response: %v\nraw: %s", err, line)
	}

	return resp
}

func initServer(t *testing.T, stdin io.Writer, stdout *bufio.Reader) {
	t.Helper()
	params := initializeParams{ProtocolVersion: "2024-11-05"}
	params.ClientInfo.Name = "test"
	sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "initialize", Params: params,
	})
}

func callTool(t *testing.T, stdin io.Writer, stdout *bufio.Reader, id int, name string, args map[string]any) callToolResult {
	t.Helper()
	resp := sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC: "2.0", ID: id, Method: "tools/call",
		Params: callToolParams{Name: name, Arguments: args},
	})
	if resp.Error != nil {
		t.Fatalf("tools/call failed: %s", resp.Error.Message)
	}
	var result callToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	return result
}

func TestInitialize(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	params := initializeParams{ProtocolVersion: "2024-11-05"}
	params.ClientInfo.Name = "test"

	resp := sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "initialize", Params: params,
	})

	if resp.Error != nil {
		t.Fatalf("initialize failed: %s", resp.Error.Message)
	}

	var result initializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.ServerInfo.Name != "jq" {
		t.Errorf("expected serverInfo.name='jq', got %q", result.ServerInfo.Name)
	}
}

func TestListTools(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	resp := sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC: "2.0", ID: 2, Method: "tools/list",
	})

	if resp.Error != nil {
		t.Fatalf("tools/list failed: %s", resp.Error.Message)
	}

	var result toolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(result.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result.Tools))
	}

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	if !names["jq"] {
		t.Error("missing 'jq' tool")
	}
	if !names["jq_read"] {
		t.Error("missing 'jq_read' tool")
	}
}

func TestJqInlineInput(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter": ".name",
		"input":  `{"name": "test", "value": 42}`,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	text := strings.TrimSpace(result.Content[0].Text)
	if text != `"test"` {
		t.Errorf("expected '\"test\"', got %q", text)
	}
}

func TestJqRawOutput(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter":     ".name",
		"input":      `{"name": "test"}`,
		"raw_output": true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	text := strings.TrimSpace(result.Content[0].Text)
	if text != "test" {
		t.Errorf("expected 'test' (no quotes), got %q", text)
	}
}

func TestJqFileInput(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	if err := os.WriteFile(tmpFile, []byte(`{"items": [1, 2, 3]}`), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter": ".items | length",
		"file":   tmpFile,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	text := strings.TrimSpace(result.Content[0].Text)
	if text != "3" {
		t.Errorf("expected '3', got %q", text)
	}
}

func TestJqReadTool(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	if err := os.WriteFile(tmpFile, []byte(`{"a":1,"b":2}`), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	result := callTool(t, stdin, stdout, 2, "jq_read", map[string]any{
		"file": tmpFile,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, `"a"`) || !strings.Contains(text, `"b"`) {
		t.Errorf("expected pretty-printed JSON, got %q", text)
	}
}

func TestJqInvalidFilter(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter": "invalid[[",
		"input":  `{"a": 1}`,
	})

	if !result.IsError {
		t.Error("expected error for invalid filter")
	}
}

func TestJqValidationBothInputs(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter": ".",
		"file":   "/tmp/test.json",
		"input":  `{}`,
	})

	if !result.IsError {
		t.Error("expected error when both file and input provided")
	}
	if !strings.Contains(result.Content[0].Text, "exactly one") {
		t.Errorf("expected validation message, got %q", result.Content[0].Text)
	}
}

func TestJqValidationNoInput(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter": ".",
	})

	if !result.IsError {
		t.Error("expected error when neither file nor input provided")
	}
}

func TestJqSlurp(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter": "length",
		"input":  "{\"a\":1}\n{\"b\":2}\n",
		"slurp":  true,
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}

	text := strings.TrimSpace(result.Content[0].Text)
	if text != "2" {
		t.Errorf("expected '2', got %q", text)
	}
}
