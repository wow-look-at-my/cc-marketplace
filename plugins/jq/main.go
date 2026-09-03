package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxOutputBytes = 1 << 20 // 1MB

// jqTools binds the resolved jq binary to the two handlers. It is a value
// rather than a package variable so a test for the not-installed path can
// build its own server without mutating state the other tests read.
type jqTools struct {
	path string
}

type JqArgs struct {
	Filter    string `json:"filter" jsonschema:"The jq filter expression to apply (e.g. '.name', '.[] | select(.age > 30)', 'keys')"`
	File      string `json:"file,omitempty" jsonschema:"Path to a JSON file to read as input. Provide exactly one of file or input."`
	Input     string `json:"input,omitempty" jsonschema:"Inline JSON string to process. Provide exactly one of file or input."`
	RawOutput bool   `json:"raw_output,omitempty" jsonschema:"If true, output raw strings without quotes (-r flag)"`
	Slurp     bool   `json:"slurp,omitempty" jsonschema:"If true, read entire input into an array (-s flag)"`
}

type JqReadArgs struct {
	File string `json:"file" jsonschema:"Path to the JSON file to read and pretty-print"`
}

func (j jqTools) runJq(ctx context.Context, _ *mcp.CallToolRequest, args JqArgs) (*mcp.CallToolResult, any, error) {
	if j.path == "" {
		return errorResult("jq is not installed. Install it with: apt install jq (Linux) or brew install jq (macOS)"), nil, nil
	}

	hasFile := args.File != ""
	hasInput := args.Input != ""
	if hasFile == hasInput {
		return errorResult("provide exactly one of 'file' or 'input', not both or neither"), nil, nil
	}

	var inputBytes []byte
	if hasFile {
		data, err := os.ReadFile(args.File)
		if err != nil {
			return errorResult(fmt.Sprintf("failed to read file: %v", err)), nil, nil
		}
		inputBytes = data
	} else {
		inputBytes = []byte(args.Input)
	}

	jqArgs := []string{}
	if args.RawOutput {
		jqArgs = append(jqArgs, "-r")
	}
	if args.Slurp {
		jqArgs = append(jqArgs, "-s")
	}
	jqArgs = append(jqArgs, args.Filter)

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, j.path, jqArgs...)
	cmd.Stdin = bytes.NewReader(inputBytes)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return errorResult(fmt.Sprintf("jq error: %s", errMsg)), nil, nil
	}

	output := stdout.Bytes()
	if len(output) > maxOutputBytes {
		output = append(output[:maxOutputBytes], []byte("\n... (output truncated, exceeded 1MB limit)")...)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(output)}},
	}, nil, nil
}

func (j jqTools) readJson(ctx context.Context, _ *mcp.CallToolRequest, args JqReadArgs) (*mcp.CallToolResult, any, error) {
	if j.path == "" {
		return errorResult("jq is not installed. Install it with: apt install jq (Linux) or brew install jq (macOS)"), nil, nil
	}

	data, err := os.ReadFile(args.File)
	if err != nil {
		return errorResult(fmt.Sprintf("failed to read file: %v", err)), nil, nil
	}

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, j.path, ".")
	cmd.Stdin = bytes.NewReader(data)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return errorResult(fmt.Sprintf("jq error: %s", errMsg)), nil, nil
	}

	output := stdout.Bytes()
	if len(output) > maxOutputBytes {
		output = append(output[:maxOutputBytes], []byte("\n... (output truncated, exceeded 1MB limit)")...)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(output)}},
	}, nil, nil
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// newServer registers both tools on a server bound to one jq binary. main and
// the tests share it, so a test drives the same registration that ships.
func newServer(tools jqTools) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "jq",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "jq",
		Description: "Run a jq expression against a JSON file or inline JSON string. Returns the filtered/transformed result as text. Cannot write to files.",
	}, tools.runJq)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "jq_read",
		Description: "Read and pretty-print a JSON file. Returns formatted JSON content.",
	}, tools.readJson)

	return server
}

func main() {
	// A lookup failure leaves the path empty, and each tool then answers
	// with the install instructions rather than the server refusing to start.
	path, _ := exec.LookPath("jq")

	if err := newServer(jqTools{path: path}).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
