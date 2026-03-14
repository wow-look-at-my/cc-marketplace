package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type HookInput struct {
	HookEventName string    `json:"hook_event_name"`
	ToolName      string    `json:"tool_name"`
	ToolInput     ToolInput `json:"tool_input"`
}

type ToolInput struct {
	FilePath string `json:"file_path"`
}

func main() {
	input, _ := io.ReadAll(os.Stdin)
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		os.Exit(0)
	}

	if hi.ToolName != "Write" {
		os.Exit(0)
	}

	path := hi.ToolInput.FilePath
	if path == "" {
		os.Exit(0)
	}

	// Check if the file already exists
	_, err := os.Stat(path)
	if err != nil {
		// File doesn't exist (or can't stat) - allow the Write
		os.Exit(0)
	}

	// File exists - block the Write
	fmt.Fprintf(os.Stderr, "BLOCKED: Cannot overwrite existing file %q with Write tool. Use the Edit tool instead to make changes to existing files.", path)
	os.Exit(2)
}
