package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

func TestMain(m *testing.M) {
	path, err := exec.LookPath("jq")
	if err == nil {
		jqPath = path
	}
	os.Exit(m.Run())
}

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()

	if jqPath == "" {
		t.Skip("jq not installed")
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "jq",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "jq",
		Description: "Run a jq expression against a JSON file or inline JSON string.",
	}, runJq)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "jq_read",
		Description: "Read and pretty-print a JSON file.",
	}, readJson)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	_, err := server.Connect(ctx, t1, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	require.NoError(t, err)

	t.Cleanup(func() { session.Close() })
	return session
}

func TestInitialize(t *testing.T) {
	session := connect(t)

	// The client is already initialized via Connect, so just verify we can list tools
	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, len(result.Tools) >= 2)
}

func TestListTools(t *testing.T) {
	session := connect(t)

	names := map[string]bool{}
	for tool, err := range session.Tools(context.Background(), nil) {
		require.NoError(t, err)
		names[tool.Name] = true
	}

	assert.True(t, names["jq"])
	assert.True(t, names["jq_read"])
}

func TestJqInlineInput(t *testing.T) {
	session := connect(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq",
		Arguments: map[string]any{
			"filter": ".name",
			"input":  `{"name": "test", "value": 42}`,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := strings.TrimSpace(result.Content[0].(*mcp.TextContent).Text)
	assert.Equal(t, `"test"`, text)
}

func TestJqRawOutput(t *testing.T) {
	session := connect(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq",
		Arguments: map[string]any{
			"filter":     ".name",
			"input":      `{"name": "test"}`,
			"raw_output": true,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := strings.TrimSpace(result.Content[0].(*mcp.TextContent).Text)
	assert.Equal(t, "test", text)
}

func TestJqFileInput(t *testing.T) {
	session := connect(t)

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	require.NoError(t, os.WriteFile(tmpFile, []byte(`{"items": [1, 2, 3]}`), 0644))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq",
		Arguments: map[string]any{
			"filter": ".items | length",
			"file":   tmpFile,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := strings.TrimSpace(result.Content[0].(*mcp.TextContent).Text)
	assert.Equal(t, "3", text)
}

func TestJqReadTool(t *testing.T) {
	session := connect(t)

	tmpFile := filepath.Join(t.TempDir(), "test.json")
	require.NoError(t, os.WriteFile(tmpFile, []byte(`{"a":1,"b":2}`), 0644))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq_read",
		Arguments: map[string]any{
			"file": tmpFile,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, `"a"`)
	assert.Contains(t, text, `"b"`)
}

func TestJqInvalidFilter(t *testing.T) {
	session := connect(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq",
		Arguments: map[string]any{
			"filter": "invalid[[",
			"input":  `{"a": 1}`,
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestJqValidationBothInputs(t *testing.T) {
	session := connect(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq",
		Arguments: map[string]any{
			"filter": ".",
			"file":   "/tmp/test.json",
			"input":  `{}`,
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, "exactly one")
}

func TestJqValidationNoInput(t *testing.T) {
	session := connect(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq",
		Arguments: map[string]any{
			"filter": ".",
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestJqReadToolFileNotFound(t *testing.T) {
	session := connect(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq_read",
		Arguments: map[string]any{
			"file": "/nonexistent/path/to/file.json",
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, "failed to read file")
}

func TestJqReadToolInvalidJSON(t *testing.T) {
	session := connect(t)

	tmpFile := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(tmpFile, []byte(`not json at all {{{`), 0644))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq_read",
		Arguments: map[string]any{
			"file": tmpFile,
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestJqFileNotFound(t *testing.T) {
	session := connect(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq",
		Arguments: map[string]any{
			"filter": ".",
			"file":   "/nonexistent/file.json",
		},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, "failed to read file")
}

func TestJqNoJqInstalled(t *testing.T) {
	// Temporarily clear jqPath to test the "not installed" path
	saved := jqPath
	jqPath = ""
	defer func() { jqPath = saved }()

	server := mcp.NewServer(&mcp.Implementation{Name: "jq", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "jq"}, runJq)
	mcp.AddTool(server, &mcp.Tool{Name: "jq_read"}, readJson)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	server.Connect(ctx, t1, nil)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	require.NoError(t, err)
	defer session.Close()

	// Test jq tool
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "jq",
		Arguments: map[string]any{"filter": ".", "input": "{}"},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, "jq is not installed")

	// Test jq_read tool
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "jq_read",
		Arguments: map[string]any{"file": "/tmp/test.json"},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, "jq is not installed")
}

func TestJqSlurp(t *testing.T) {
	session := connect(t)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "jq",
		Arguments: map[string]any{
			"filter": "length",
			"input":  "{\"a\":1}\n{\"b\":2}\n",
			"slurp":  true,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text := strings.TrimSpace(result.Content[0].(*mcp.TextContent).Text)
	assert.Equal(t, "2", text)
}
