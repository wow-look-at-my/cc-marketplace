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
	Type string `json:"type"`
	Name string `json:"name"`
	// ID pairs a tool_use with the tool_result that answers it, which is how a
	// TaskCreate's subject is matched to the id its result reports.
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Input     json.RawMessage `json:"input"`
}

// transcriptRecord is one transcript line reduced to the content blocks the
// gate cares about, so the file is read once and both the TodoWrite and the
// task-tool reconstruction work from the same pass.
type transcriptRecord struct {
	Blocks []contentBlock
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

// readTranscript reads the JSONL transcript once, returning each line's content
// blocks in file order. A line that is not a message, or whose content is a
// plain string rather than an array of blocks, carries no tool calls and is
// skipped.
func readTranscript(path string) []transcriptRecord {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var records []transcriptRecord
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
		records = append(records, transcriptRecord{Blocks: blocks})
	}
	return records
}

// latestTodos returns the todo list from the most recent TodoWrite call. Each
// call replaces the whole list, so the last one wins. Environments with the
// task tools instead of TodoWrite yield nothing here; see latestTasks.
func latestTodos(records []transcriptRecord) []TodoItem {
	var todos []TodoItem
	for _, rec := range records {
		for _, b := range rec.Blocks {
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
	b.WriteString("\nThe only legitimate way past this point is a task list with no pending or in-progress items. If a task is genuinely done, mark it completed (TaskUpdate status completed, or TodoWrite). If it is no longer applicable, delete it the same way. Reflect reality in the list -- do not just stop on top of unfinished work.")
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

	// Both sources, one pass. An environment has one or the other -- TodoWrite
	// or the task tools -- but reading both means the gate does not silently
	// stop guarding when the tool surface changes underneath it, which is
	// exactly what happened when the task tools replaced TodoWrite.
	records := readTranscript(hi.TranscriptPath)
	inProgress, pending := incompleteTodos(latestTodos(records))
	taskInProgress, taskPending := incompleteTasks(latestTasks(records))
	inProgress = append(inProgress, taskInProgress...)
	pending = append(pending, taskPending...)
	if len(inProgress)+len(pending) == 0 {
		return 0, ""
	}
	return 2, blockReason(inProgress, pending)
}

// run reads the hook payload from r and returns the exit code, the stderr
// message (a Stop block reports that way) and the stdout payload (the
// entry-side hooks answer in JSON).
//
// One binary serves all three events, dispatched on hook_event_name: they share
// the debt marker and the task-tool vocabulary, so splitting them into separate
// programs is what let half this plugin drift into another language.
func run(r io.Reader) (code int, stderr, stdout string) {
	input, _ := io.ReadAll(r)
	payload := parsePayload(input)

	switch payload.EventName {
	case "UserPromptSubmit":
		return 0, "", promptArm(payload)
	case "PreToolUse":
		return 0, "", todoGate(payload)
	default:
		// Stop, and anything unrecognized: an unknown event must not block.
		c, msg := evaluate(input)
		return c, msg, ""
	}
}

func main() {
	code, stderr, stdout := run(os.Stdin)
	if stdout != "" {
		fmt.Print(stdout)
	}
	if stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}
	os.Exit(code)
}
