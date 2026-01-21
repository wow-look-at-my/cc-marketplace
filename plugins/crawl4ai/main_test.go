package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
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

func startServer(t *testing.T) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
	t.Helper()

	exe := "./crawl4ai-mcp"
	if _, err := os.Stat(exe); err != nil {
		t.Skip("crawl4ai-mcp not built")
	}

	cmd := exec.Command(exe)
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

func TestInitialize(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	params := initializeParams{
		ProtocolVersion: "2024-11-05",
	}
	params.ClientInfo.Name = "test"

	resp := sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  params,
	})

	if resp.Error != nil {
		t.Fatalf("initialize failed: %s", resp.Error.Message)
	}

	var result initializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if result.ServerInfo.Name != "crawl4ai" {
		t.Errorf("expected serverInfo.name='crawl4ai', got %q", result.ServerInfo.Name)
	}
}

func TestListTools(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	// Initialize first
	params := initializeParams{
		ProtocolVersion: "2024-11-05",
	}
	params.ClientInfo.Name = "test"

	sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  params,
	})

	// List tools
	resp := sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	})

	if resp.Error != nil {
		t.Fatalf("tools/list failed: %s", resp.Error.Message)
	}

	var result toolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}

	if result.Tools[0].Name != "crawl" {
		t.Errorf("expected tool name='crawl', got %q", result.Tools[0].Name)
	}
}
