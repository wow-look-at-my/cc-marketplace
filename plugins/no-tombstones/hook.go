// Command no-tombstones is a Claude Code PreToolUse hook that refuses a write
// adding a tombstone comment.
//
// A tombstone describes a state the code is no longer in, or argues for the
// diff instead of telling the next editor what breaks. Both kinds read as
// authority: nobody re-derives a comment, so a comment about a tree that no
// longer exists misleads for as long as it survives. The test is one step --
// delete the sentence and ask what the next editor gets wrong. No answer means
// it was narration.
//
// Only text the write ADDS is judged, so a tombstone already in a file is never
// a reason to refuse an unrelated edit to it.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
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

// defaultMaxCommentLines caps one comment block in source. Volume is the tier
// no rewording defeats: a tombstone is surplus text, so an essay whose every
// sentence reads as true and current still fails here. Override with
// NO_TOMBSTONES_MAX_COMMENT_LINES; 0 turns the cap off.
const defaultMaxCommentLines = 14

func main() {
	if out := run(os.Stdin); out != "" {
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

	text := added(ti)
	blocks := AddedBlocks(ti.FilePath, text)
	if len(blocks) == 0 {
		return ""
	}

	// The volume cap governs source only. A long paragraph in a document is
	// ordinary writing; a long comment block attached to a declaration is the
	// essay this plugin exists to stop.
	limit := maxCommentLines()
	if IsDocument(ti.FilePath) {
		limit = 0
	}

	hits := FindTombstones(blocks, limit)
	for _, name := range DeadReferents(ti.FilePath, text, blocks) {
		hits = append(hits, Hit{
			Tell:   "a name nothing in the repository defines",
			Phrase: name,
			Line:   lineNaming(blocks, name),
		})
	}
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

func maxCommentLines() int {
	raw := os.Getenv("NO_TOMBSTONES_MAX_COMMENT_LINES")
	if raw == "" {
		return defaultMaxCommentLines
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultMaxCommentLines
	}
	return n
}

// lineNaming finds the comment line a dead name sits on, so the refusal can
// quote it like every other finding.
func lineNaming(blocks []Block, name string) string {
	for _, b := range blocks {
		for _, line := range strings.Split(b.Text, "\n") {
			if strings.Contains(line, name) {
				return strings.TrimSpace(line)
			}
		}
	}
	return name
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

// reason is what the model is told: each finding with the tell that caught it,
// where the text went, and the shape to write instead.
func reason(path string, hits []Hit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "blocked: this write adds a tombstone comment to %s.\n\n", path)
	const shown = 6
	for _, h := range hits[:min(len(hits), shown)] {
		fmt.Fprintf(&b, "  %s: %q\n      %s\n", h.Tell, h.Phrase, h.Line)
	}
	// Say what was not printed. A silent truncation reads as the whole list,
	// so the next write fixes six findings and is refused again.
	if len(hits) > shown {
		fmt.Fprintf(&b, "  ... and %d more, not listed.\n", len(hits)-shown)
	}
	if dest := Relocate(path, hits); dest != "" {
		fmt.Fprintf(&b, "\nThe refused lines are appended to %s. Nothing is lost:\n"+
			"put them in the commit message, where narrating a change belongs.\n", dest)
	}
	b.WriteString(remedy)
	return b.String()
}

// remedy is the standing half of the refusal.
const remedy = `
A comment carries current truth. It states an invariant the next edit breaks, a
footgun, an ordering constraint, or why the obvious approach is wrong. It does
not carry what the code used to be, when it changed, who asked, or why the diff
deserves to land -- git already holds all of that, and a comment is the one
place it cannot be queried and cannot be trusted to stay true.

Test each sentence: delete it, and say what the next editor now gets wrong. No
answer means it was narration.

Rewrite the comment as what the code IS, then write the file.`
