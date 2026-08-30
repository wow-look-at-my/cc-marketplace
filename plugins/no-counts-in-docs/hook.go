// Command no-counts-in-docs is a Claude Code PreToolUse hook that refuses a
// write introducing a count into a markdown document.
//
// A count is a claim about how many things exist at the moment it was typed.
// The edit that adds an item leaves it wrong, and nothing in the repository
// says so: the reader trusts the number for as long as it survives. Describing
// what is there instead -- "every plugin this repo installs" rather than "this
// repo's 15 plugins" -- stays true through the next edit.
//
// Only text the write ADDS is judged, so an existing count in a file is never
// a reason to refuse an unrelated edit to it.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// HookInput is the subset of the PreToolUse payload this plugin reads.
type HookInput struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// toolInput carries the write shapes of Write, Edit and MultiEdit together;
// each tool fills the fields it has and leaves the rest empty.
type toolInput struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`
	NewString string `json:"new_string"`
	Edits     []struct {
		NewString string `json:"new_string"`
	} `json:"edits"`
}

// response is the deny payload. An allow is the empty output: this hook has no
// business granting permission, only refusing.
type response struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func main() {
	out := run(os.Stdin)
	if out != "" {
		fmt.Print(out)
	}
}

// run reads a hook payload from r and returns the JSON to print, or "" to let
// the call through. Every failure path returns "": a guard that blocks because
// it could not parse its own input is worse than no guard.
func run(r io.Reader) string {
	data, _ := io.ReadAll(r)
	var in HookInput
	if json.Unmarshal(data, &in) != nil {
		return ""
	}
	if in.HookEventName != "" && in.HookEventName != "PreToolUse" {
		return ""
	}
	if in.ToolName != "Write" && in.ToolName != "Edit" && in.ToolName != "MultiEdit" {
		return ""
	}
	var ti toolInput
	if json.Unmarshal(in.ToolInput, &ti) != nil {
		return ""
	}
	if !IsMarkdown(ti.FilePath) {
		return ""
	}
	hits := FindCounts(added(ti))
	if len(hits) == 0 {
		return ""
	}
	return deny(reason(ti.FilePath, hits))
}

// added joins the text this write puts into the file.
func added(ti toolInput) string {
	parts := []string{ti.Content, ti.NewString}
	for _, e := range ti.Edits {
		parts = append(parts, e.NewString)
	}
	return strings.Join(parts, "\n")
}

func deny(why string) string {
	var res response
	res.HookSpecificOutput.HookEventName = "PreToolUse"
	res.HookSpecificOutput.PermissionDecision = "deny"
	res.HookSpecificOutput.PermissionDecisionReason = why
	out, err := json.Marshal(res)
	if err != nil {
		return ""
	}
	return string(out)
}

// reason is what the model is told. It quotes each count with the line it sits
// on and says what to write instead, because a refusal that does not say what
// to write costs a round trip to find out.
func reason(path string, hits []Hit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "blocked: this write states a count in %s.\n\n", path)
	for _, hit := range hits[:min(len(hits), 6)] {
		fmt.Fprintf(&b, "  %q\n      %s\n", hit.Phrase, hit.Line)
	}
	b.WriteString(remedy)
	return b.String()
}

// remedy is the standing half of the refusal: why a count rots, and the shape
// to write instead.
const remedy = `
A count is true only until somebody adds or removes an item, and nothing in the
repository corrects it when they do -- the reader keeps trusting a number that
has quietly gone wrong. Describe what is there and let the reader count:
"every plugin this repo installs", not "this repo's 15 plugins"; "the rules
below", not "the four rules below".

Rewrite the text without the count, then write the file.`
