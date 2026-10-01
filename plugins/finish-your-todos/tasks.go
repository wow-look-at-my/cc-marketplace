package main

// Task-tool support for the Stop gate.
//
// The original hook read TodoWrite calls, where each call carries the whole
// list, so the last one wins. The task tools work differently: TaskCreate files
// one task and returns its id in the RESULT, TaskUpdate changes one task's
// status by id. State therefore has to be reconstructed across the transcript
// rather than read off the last call.
//
// This is not a nice-to-have. Environments that expose TaskCreate/TaskUpdate
// have no TodoWrite at all, so on them the Stop gate was scanning for a tool
// that is never called and allowing every stop -- a guard that had quietly
// stopped guarding.

import (
	"encoding/json"
	"regexp"
	"strings"
)

// taskState is one task as reconstructed from the transcript.
type taskState struct {
	ID      string
	Subject string
	Status  string
}

// taskCreateInput is the input of a TaskCreate call. The id is not here -- it
// comes back in the result -- but the subject is, which is what a block message
// needs to name the work.
type taskCreateInput struct {
	Subject string `json:"subject"`
}

// taskUpdateInput is the input of a TaskUpdate call.
type taskUpdateInput struct {
	TaskID  string `json:"taskId"`
	Status  string `json:"status"`
	Subject string `json:"subject"`
}

// "Task #6 created successfully: Subject here"
var taskCreatedRE = regexp.MustCompile(`Task #(\d+) created`)

// latestTasks reconstructs the task list from the transcript in file order:
// a TaskCreate's result supplies the id for the subject its call carried, and
// each later TaskUpdate rewrites that task's status.
func latestTasks(lines []transcriptRecord) []taskState {
	order := []string{}
	byID := map[string]*taskState{}
	// tool_use_id -> subject, for TaskCreate calls whose result has not been
	// seen yet.
	pending := map[string]string{}

	for _, rec := range lines {
		for _, b := range rec.Blocks {
			switch {
			case b.Type == "tool_use" && b.Name == "TaskCreate":
				var in taskCreateInput
				if err := json.Unmarshal(b.Input, &in); err != nil {
					continue
				}
				if b.ID != "" {
					pending[b.ID] = in.Subject
				}

			case b.Type == "tool_use" && b.Name == "TaskUpdate":
				var in taskUpdateInput
				if err := json.Unmarshal(b.Input, &in); err != nil {
					continue
				}
				t, ok := byID[in.TaskID]
				if !ok {
					continue
				}
				if in.Status != "" {
					t.Status = in.Status
				}
				if in.Subject != "" {
					t.Subject = in.Subject
				}

			case b.Type == "tool_result" && b.ToolUseID != "":
				subject, ok := pending[b.ToolUseID]
				if !ok {
					continue
				}
				delete(pending, b.ToolUseID)
				m := taskCreatedRE.FindStringSubmatch(resultText(b.Content))
				if m == nil {
					continue
				}
				id := m[1]
				if _, exists := byID[id]; !exists {
					order = append(order, id)
					byID[id] = &taskState{ID: id, Subject: subject, Status: "pending"}
				}
			}
		}
	}

	out := make([]taskState, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

// resultText flattens a tool_result's content, which is either a plain string
// or an array of blocks each carrying text.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// incompleteTasks splits reconstructed tasks the same way incompleteTodos does.
// An unrecognized status counts as finished, so the gate fails open rather than
// blocking on a state it does not understand.
func incompleteTasks(tasks []taskState) (inProgress, pending []TodoItem) {
	for _, t := range tasks {
		item := TodoItem{Content: t.Subject, Status: t.Status}
		switch t.Status {
		case "in_progress":
			inProgress = append(inProgress, item)
		case "pending":
			pending = append(pending, item)
		}
	}
	return inProgress, pending
}
