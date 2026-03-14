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

// evaluate checks whether a Write tool call should be blocked.
// Returns exit code (0 = allow, 2 = block) and a message for stderr when blocking.
func evaluate(input []byte) (int, string) {
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		return 0, ""
	}

	if hi.ToolName != "Write" {
		return 0, ""
	}

	path := hi.ToolInput.FilePath
	if path == "" {
		return 0, ""
	}

	// Check if the file already exists
	_, err := os.Stat(path)
	if err != nil {
		// File doesn't exist (or can't stat) - allow the Write
		return 0, ""
	}

	// File exists - block the Write
	return 2, fmt.Sprintf("BLOCKED: Cannot overwrite existing file %q with Write tool. Use the Edit tool instead to make changes to existing files.", path)
}

func main() {
	input, _ := io.ReadAll(os.Stdin)
	code, msg := evaluate(input)
	if msg != "" {
		fmt.Fprint(os.Stderr, msg)
	}
	os.Exit(code)
}
