// The entry-side halves of the plugin: arm on the prompt, collect on the tool.
//
// Splitting it this way is forced by the hook surface. UserPromptSubmit cannot
// refuse a tool call -- all it can do is inject context, which is exactly the
// kind of advice that has been ignored all along -- so it only records the
// debt. PreToolUse is where the refusal happens.

package main

import (
	"encoding/json"
	"fmt"
)

const armedContext = "This message assigns work. File it with TaskCreate before doing anything else -- " +
	"every other tool is refused until you do. If it maps onto a task that already exists, " +
	"TaskUpdate that one (status in_progress, or an amended description) instead of filing a duplicate."

// promptArm handles UserPromptSubmit. It returns the stdout payload, which is
// empty on every path that does not arm.
func promptArm(p hookPayload) string {
	if p.SessionID == "" || !assignsWork(p.Prompt) {
		return ""
	}
	// An outstanding debt stays as it is: the first unfiled assignment is the
	// one to name, and overwriting it would lose it behind a follow-up.
	if readDebt(p.SessionID) != nil {
		return ""
	}
	writeDebt(p.SessionID, Debt{Prompt: summarize(p.Prompt), Refusals: 0})

	return encodeHook(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": armedContext,
		},
	})
}

// todoGate handles PreToolUse for every tool. While a session owes a task,
// every tool except the task tools is DENIED -- a hard permissionDecision
// rather than injected advice, because the model already receives a system
// reminder about the task list on most turns and reads past it.
//
// The debt is settled by TaskCreate (new work) or TaskUpdate (work that maps
// onto a task already filed). TaskList and TaskGet stay callable while blocked
// so the settling call can check for duplicates first. There is deliberately
// no "declare that there is no task" escape: an escape hatch is the hole the
// whole plugin exists to close.
func todoGate(p hookPayload) string {
	if p.SessionID == "" {
		return ""
	}
	// A message sent mid-turn is enqueued and folded into the running turn as
	// an attachment, and that path dispatches no UserPromptSubmit at all -- so
	// promptArm never saw it. On a web surface every inbound message goes
	// through that queue whenever the session is busy, which is most of the
	// time. PreToolUse is the only event that fires regardless of how the
	// message arrived, so the catch-up happens here.
	armFromTranscript(p.SessionID, p.TranscriptPath)

	debt := readDebt(p.SessionID)
	if debt == nil {
		return ""
	}
	if settlingTools[p.ToolName] {
		clearDebt(p.SessionID)
		return ""
	}
	if taskTools[p.ToolName] {
		return ""
	}

	writeDebt(p.SessionID, Debt{Prompt: debt.Prompt, Refusals: debt.Refusals + 1})

	nagged := ""
	if debt.Refusals > 0 {
		nagged = fmt.Sprintf(" This is refusal %d for the same outstanding task; nothing else will run until it is filed.", debt.Refusals+1)
	}

	return encodeHook(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":      "PreToolUse",
			"permissionDecision": "deny",
			"permissionDecisionReason": "Blocked: the user assigned work and no task was filed for it. Call TaskCreate now, with a subject naming " +
				"the outcome, then retry this tool call -- it will go through unchanged. If the work maps onto a task that " +
				"already exists, TaskUpdate that one instead. The assignment was: “" + debt.Prompt + "”." + nagged,
		},
	})
}

// encodeHook renders a hook response, returning "" if it cannot be marshalled
// so a broken guard stays fail-open.
func encodeHook(v map[string]any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data) + "\n"
}
