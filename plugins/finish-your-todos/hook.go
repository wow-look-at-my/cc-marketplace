// Command finish-your-todos is a Claude Code Stop hook that blocks the assistant
// from ending its turn while its TodoWrite list still has incomplete items.
//
// It reads the Stop hook payload on stdin, finds the most recent TodoWrite tool
// call in the transcript (each call carries the complete list), and if any item
// is still "pending" or "in_progress" it blocks the stop (exit 2) with a reason
// naming the unfinished work. The stop_hook_active flag is honored as a loop
// guard: once Claude is already continuing because of a prior block, the stop is
// allowed through so a genuinely stuck session can never hang forever.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// HookInput is the JSON payload Claude Code delivers on stdin for a Stop hook.
// Fields mirror the CLI's hook schema (session_id/transcript_path/cwd/
// permission_mode plus the Stop-specific hook_event_name and stop_hook_active).
type HookInput struct {
	HookEventName  string `json:"hook_event_name"`
	TranscriptPath string `json:"transcript_path"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// transcriptLine is one JSONL record in the conversation transcript.
type transcriptLine struct {
	Message json.RawMessage `json:"message"`
}

// transcriptMessage is the inner message object. Content is a raw message
// because it may be a string (plain text) or an array of content blocks.
type transcriptMessage struct {
	Content json.RawMessage `json:"content"`
}

// contentBlock is one block inside an assistant message's content array. Only
// tool_use blocks carry a tool name and input.
type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// todoWriteInput is the input object of a TodoWrite tool call.
type todoWriteInput struct {
	Todos []TodoItem `json:"todos"`
}

// TodoItem matches the TodoWrite schema: content (imperative), status, and
// activeForm (present continuous). Status is one of pending/in_progress/
// completed.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

// latestTodos returns the todo list from the most recent TodoWrite call in the
// transcript, or nil if the file can't be read or no TodoWrite call is present.
// Each TodoWrite call replaces the whole list, so the last one wins.
func latestTodos(path string) []TodoItem {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var todos []TodoItem
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		var line transcriptLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if len(line.Message) == 0 {
			continue
		}
		var msg transcriptMessage
		if err := json.Unmarshal(line.Message, &msg); err != nil {
			continue
		}
		var blocks []contentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			// Content is a plain string (not an array of blocks); no tool calls.
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_use" || b.Name != "TodoWrite" {
				continue
			}
			var in todoWriteInput
			if err := json.Unmarshal(b.Input, &in); err != nil {
				continue
			}
			todos = in.Todos
		}
	}
	return todos
}

// incompleteTodos splits a todo list into the in-progress and pending items
// (everything that is not "completed"). Anything with an unrecognized status is
// treated as complete so the hook fails open rather than blocking forever.
func incompleteTodos(todos []TodoItem) (inProgress, pending []TodoItem) {
	for _, t := range todos {
		switch t.Status {
		case "in_progress":
			inProgress = append(inProgress, t)
		case "pending":
			pending = append(pending, t)
		}
	}
	return inProgress, pending
}

// blockReason builds the message shown to Claude when the stop is blocked.
func blockReason(inProgress, pending []TodoItem) string {
	var b strings.Builder
	total := len(inProgress) + len(pending)
	plural := "item"
	if total != 1 {
		plural = "items"
	}
	fmt.Fprintf(&b, "STOP. You are trying to end your turn, but your todo list still has %d incomplete %s. Do not stop now -- finish the work you started.\n", total, plural)

	if len(inProgress) > 0 {
		b.WriteString("\nIn progress:\n")
		for _, t := range inProgress {
			fmt.Fprintf(&b, "  - %s\n", t.Content)
		}
	}
	if len(pending) > 0 {
		b.WriteString("\nNot started:\n")
		for _, t := range pending {
			fmt.Fprintf(&b, "  - %s\n", t.Content)
		}
	}

	b.WriteString("\nStopping now would leave the user's request half-finished -- exactly the kind of accidental abandonment this guard exists to catch. Keep going and complete these tasks.\n")
	b.WriteString("\nThe only legitimate way past this point is a todo list with no pending or in-progress items. If a task is genuinely done, mark it completed with TodoWrite. If a task is no longer applicable, remove it with TodoWrite. Reflect reality in the list -- do not just stop on top of unfinished work.")
	return b.String()
}

// evaluate decides whether to allow or block the stop. It returns the process
// exit code (0 = allow stop, 2 = block stop) and, when blocking, the reason
// written to stderr for Claude to read.
func evaluate(input []byte) (int, string) {
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		return 0, ""
	}
	if hi.HookEventName != "Stop" {
		return 0, ""
	}
	// Loop guard: if we are already continuing because of a previous block,
	// allow the stop so a stuck session cannot hang indefinitely.
	if hi.StopHookActive {
		return 0, ""
	}

	inProgress, pending := incompleteTodos(latestTodos(hi.TranscriptPath))
	if len(inProgress)+len(pending) == 0 {
		return 0, ""
	}
	return 2, blockReason(inProgress, pending)
}

// run reads the hook payload from r and returns the exit code and stderr message.
func run(r io.Reader) (int, string) {
	input, _ := io.ReadAll(r)
	return evaluate(input)
}

func main() {
	code, msg := run(os.Stdin)
	if msg != "" {
		fmt.Fprint(os.Stderr, msg)
	}
	os.Exit(code)
}
