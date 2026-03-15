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
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

type jsonRPCRequest struct {
	JSONRPC	string	`json:"jsonrpc"`
	ID	int	`json:"id"`
	Method	string	`json:"method"`
	Params	any	`json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC	string		`json:"jsonrpc"`
	ID	int		`json:"id"`
	Result	json.RawMessage	`json:"result"`
	Error	*struct {
		Code	int	`json:"code"`
		Message	string	`json:"message"`
	}	`json:"error"`
}

type initializeParams struct {
	ProtocolVersion	string	`json:"protocolVersion"`
	ClientInfo	struct {
		Name string `json:"name"`
	}	`json:"clientInfo"`
	Capabilities	struct{}	`json:"capabilities"`
}

type initializeResult struct {
	ServerInfo struct {
		Name	string	`json:"name"`
		Version	string	`json:"version"`
	} `json:"serverInfo"`
}

type toolsListResult struct {
	Tools []struct {
		Name		string	`json:"name"`
		Description	string	`json:"description"`
	} `json:"tools"`
}

type callToolParams struct {
	Name		string		`json:"name"`
	Arguments	map[string]any	`json:"arguments"`
}

type callToolResult struct {
	Content	[]struct {
		Type	string	`json:"type"`
		Text	string	`json:"text"`
	}	`json:"content"`
	IsError	bool	`json:"isError"`
}

func startServer(t *testing.T) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
	t.Helper()

	cmd := exec.Command("./run")
	stdin, err := cmd.StdinPipe()
	require.Nil(t, err)

	stdout, err := cmd.StdoutPipe()
	require.Nil(t, err)

	require.NoError(t, cmd.Start())

	return cmd, stdin, bufio.NewReader(stdout)
}

func sendRequest(t *testing.T, stdin io.Writer, stdout *bufio.Reader, req jsonRPCRequest) jsonRPCResponse {
	t.Helper()

	data, err := json.Marshal(req)
	require.Nil(t, err)

	_, err = stdin.Write(append(data, '\n'))
	require.Nil(t, err)

	line, err := stdout.ReadBytes('\n')
	require.Nil(t, err)

	var resp jsonRPCResponse
	require.NoError(t, json.Unmarshal(line, &resp))

	return resp
}

func initServer(t *testing.T, stdin io.Writer, stdout *bufio.Reader) {
	t.Helper()
	params := initializeParams{ProtocolVersion: "2024-11-05"}
	params.ClientInfo.Name = "test"
	sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC:	"2.0", ID: 1, Method: "initialize", Params: params,
	})
}

func callTool(t *testing.T, stdin io.Writer, stdout *bufio.Reader, id int, name string, args map[string]any) callToolResult {
	t.Helper()
	resp := sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC:	"2.0", ID: id, Method: "tools/call",
		Params:	callToolParams{Name: name, Arguments: args},
	})
	require.Nil(t, resp.Error)

	var result callToolResult
	require.NoError(t, json.Unmarshal(resp.Result, &result))

	return result
}

func TestInitialize(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	params := initializeParams{ProtocolVersion: "2024-11-05"}
	params.ClientInfo.Name = "test"

	resp := sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC:	"2.0", ID: 1, Method: "initialize", Params: params,
	})

	require.Nil(t, resp.Error)

	var result initializeResult
	require.NoError(t, json.Unmarshal(resp.Result, &result))

	assert.Equal(t, "jq", result.ServerInfo.Name)

}

func TestListTools(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	resp := sendRequest(t, stdin, stdout, jsonRPCRequest{
		JSONRPC:	"2.0", ID: 2, Method: "tools/list",
	})

	require.Nil(t, resp.Error)

	var result toolsListResult
	require.NoError(t, json.Unmarshal(resp.Result, &result))

	require.Equal(t, 2, len(result.Tools))

	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	assert.True(t, names["jq"])

	assert.True(t, names["jq_read"])

}

func TestJqInlineInput(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter":	".name",
		"input":	`{"name": "test", "value": 42}`,
	})

	require.False(t, result.IsError)

	text := strings.TrimSpace(result.Content[0].Text)
	assert.Equal(t, `"test"`, text)

}

func TestJqRawOutput(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter":	".name",
		"input":	`{"name": "test"}`,
		"raw_output":	true,
	})

	require.False(t, result.IsError)

	text := strings.TrimSpace(result.Content[0].Text)
	assert.Equal(t, "test", text)

}

func TestJqFileInput(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	require.NoError(t, os.WriteFile(tmpFile, []byte(`{"items": [1, 2, 3]}`), 0644))

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter":	".items | length",
		"file":		tmpFile,
	})

	require.False(t, result.IsError)

	text := strings.TrimSpace(result.Content[0].Text)
	assert.Equal(t, "3", text)

}

func TestJqReadTool(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	require.NoError(t, os.WriteFile(tmpFile, []byte(`{"a":1,"b":2}`), 0644))

	result := callTool(t, stdin, stdout, 2, "jq_read", map[string]any{
		"file": tmpFile,
	})

	require.False(t, result.IsError)

	text := result.Content[0].Text
	assert.False(t, !strings.Contains(text, `"a"`) || !strings.Contains(text, `"b"`))

}

func TestJqInvalidFilter(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter":	"invalid[[",
		"input":	`{"a": 1}`,
	})

	assert.True(t, result.IsError)

}

func TestJqValidationBothInputs(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter":	".",
		"file":		"/tmp/test.json",
		"input":	`{}`,
	})

	assert.True(t, result.IsError)

	assert.Contains(t, result.Content[0].Text, "exactly one")

}

func TestJqValidationNoInput(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter": ".",
	})

	assert.True(t, result.IsError)

}

func TestJqSlurp(t *testing.T) {
	cmd, stdin, stdout := startServer(t)
	defer cmd.Process.Kill()
	defer stdin.Close()

	initServer(t, stdin, stdout)

	result := callTool(t, stdin, stdout, 2, "jq", map[string]any{
		"filter":	"length",
		"input":	"{\"a\":1}\n{\"b\":2}\n",
		"slurp":	true,
	})

	require.False(t, result.IsError)

	text := strings.TrimSpace(result.Content[0].Text)
	assert.Equal(t, "2", text)

}
