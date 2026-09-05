package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/go-containers/set"
)

func TestMain(m *testing.M) {
	path, err := ensureJq()
	if err != nil {
		fmt.Fprintf(os.Stderr, "jq bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	jqPath = path
	os.Exit(m.Run())
}

// bootstrapJqVersion pins the jq the suite fetches when the machine has
// none. jq is MIT licensed.
const bootstrapJqVersion = "1.7.1"

// ensureJq returns a jq to test against, fetching a pinned one when the
// machine has none.
//
// Letting jqPath stay empty instead turns every tool call into "jq is not
// installed", which is a valid answer the server really gives -- so the
// suite does not error, it just asserts that answer against tests written
// for a working jq, and reports a runner without jq as a bug in this
// plugin. The runner image is not this suite's contract to depend on; the
// sibling grep and glob plugins bootstrap a pinned ripgrep for the same
// reason.
func ensureJq() (string, error) {
	if path, err := exec.LookPath("jq"); err == nil {
		return path, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "cc-jq-plugin", "jq-"+bootstrapJqVersion)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "jq")
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	asset, err := jqReleaseAsset()
	if err != nil {
		return "", err
	}
	url := "https://github.com/jqlang/jq/releases/download/jq-" + bootstrapJqVersion + "/" + asset
	if err := downloadExecutable(url, bin); err != nil {
		return "", err
	}
	return bin, nil
}

// jqReleaseAsset names the single-file binary for this platform. jq ships
// one executable per platform rather than an archive, so there is nothing
// to unpack.
func jqReleaseAsset() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "jq-linux-amd64", nil
	case "linux/arm64":
		return "jq-linux-arm64", nil
	case "darwin/amd64":
		return "jq-macos-amd64", nil
	case "darwin/arm64":
		return "jq-macos-arm64", nil
	}
	return "", fmt.Errorf("no pinned jq for %s/%s; install jq to run these tests", runtime.GOOS, runtime.GOARCH)
}

func downloadExecutable(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	// Written beside the destination and renamed, so a download killed part
	// way through never leaves a truncated binary that later runs treat as
	// a cache hit.
	tmp, err := os.CreateTemp(filepath.Dir(dest), "jq-download-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
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

	names := set.New[string]()
	for tool, err := range session.Tools(context.Background(), nil) {
		require.NoError(t, err)
		names.Add(tool.Name)
	}

	assert.True(t, names.Contains("jq"))
	assert.True(t, names.Contains("jq_read"))
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
