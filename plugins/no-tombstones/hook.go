// Command no-tombstones is a Claude Code PreToolUse hook that strips a
// tombstone comment out of a write rather than refusing it.
//
// A tombstone describes a state the code is no longer in, or argues for the
// diff instead of telling the next editor what breaks. Both kinds read as
// authority: nobody re-derives a comment, so a comment about a tree that no
// longer exists misleads for as long as it survives. The test is one step --
// delete the sentence and ask what the next editor gets wrong. No answer means
// it was narration.
//
// When every finding maps to one whole, comment-only source line, that line is
// deleted and the write proceeds with the rest untouched. A finding that does
// not resolve to a clean line -- a code line carrying a trailing tombstone
// comment, a document sentence sharing a line with other prose, or the volume
// cap's judgement that a whole block is too long to excise piecemeal -- still
// gets the write refused, because a strip that guesses wrong corrupts the
// file worse than a round trip back to the model does.
//
// Only text the write ADDS is judged, so a tombstone already in a file is never
// a reason to touch an unrelated edit to it.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
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

// response covers both outcomes this hook can produce. A deny sets only the
// permission fields; a strip sets only UpdatedInput plus AdditionalContext.
// The empty response is the third outcome -- allow, unchanged -- and is never
// printed at all.
type response struct {
	HookSpecificOutput struct {
		HookEventName            string          `json:"hookEventName"`
		PermissionDecision       string          `json:"permissionDecision,omitempty"`
		PermissionDecisionReason string          `json:"permissionDecisionReason,omitempty"`
		UpdatedInput             json.RawMessage `json:"updatedInput,omitempty"`
		AdditionalContext        string          `json:"additionalContext,omitempty"`
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

// unit is one string this write actually replaces: Write's content, Edit's
// new_string, or one MultiEdit entry's new_string. apply writes the stripped
// text back into the generic map that becomes updatedInput, so every other
// field of the original tool_input (old_string, replace_all, ...) survives
// untouched.
type unit struct {
	text  string
	apply func(stripped string)
}

// writeUnits reads the units toolName's own shape carries. raw is the same
// tool_input decoded generically, mutated in place by each unit's apply.
func writeUnits(toolName string, ti toolInput, raw map[string]any) []unit {
	switch toolName {
	case "Write":
		return []unit{{text: ti.Content, apply: func(s string) { raw["content"] = s }}}
	case "Edit":
		return []unit{{text: ti.NewString, apply: func(s string) { raw["new_string"] = s }}}
	case "MultiEdit":
		rawEdits, _ := raw["edits"].([]any)
		var units []unit
		for i := range ti.Edits {
			i := i
			units = append(units, unit{
				text: ti.Edits[i].NewString,
				apply: func(s string) {
					if i >= len(rawEdits) {
						return
					}
					if m, ok := rawEdits[i].(map[string]any); ok {
						m["new_string"] = s
					}
				},
			})
		}
		return units
	default:
		return nil
	}
}

// run reads a hook payload from r and returns the JSON to print, or "" to let
// the call through unchanged. Every failure path returns "": a guard that
// blocks because it could not parse its own input is worse than no guard.
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
	var raw map[string]any
	if json.Unmarshal(in.ToolInput, &raw) != nil {
		return ""
	}

	limit := maxCommentLines()
	isDoc := IsDocument(ti.FilePath)
	if isDoc {
		limit = 0
	}

	var scans []scanned
	var allHits []Hit
	for _, u := range writeUnits(in.ToolName, ti, raw) {
		blocks := AddedBlocks(ti.FilePath, u.text)
		if len(blocks) == 0 {
			continue
		}
		hits := FindTombstones(blocks, limit)
		for _, name := range DeadReferents(ti.FilePath, u.text, blocks) {
			hits = append(hits, hitForName(blocks, name))
		}
		if len(hits) == 0 {
			continue
		}
		if isDoc {
			// A document line is a paragraph, not a sentence: the org's own
			// no-hard-wrap convention means several sentences often share
			// one raw line, so deleting the line can take a keeper with it.
			// Never guess at the sentence boundary; always send this back.
			for i := range hits {
				hits[i].Strippable = false
			}
		}
		scans = append(scans, scanned{u: u, hits: hits})
		allHits = append(allHits, hits...)
	}
	if len(allHits) == 0 {
		return ""
	}

	for _, h := range allHits {
		if !h.Strippable {
			return deny(reason(ti.FilePath, allHits))
		}
	}
	return strip(ti.FilePath, raw, scans, allHits)
}

// scanned pairs a unit with the hits found in its own text.
type scanned struct {
	u    unit
	hits []Hit
}

// strip deletes every hit's source line from its own unit and returns the
// allow-with-updatedInput response. It is reached only once every hit in
// scans has already been proven strippable.
func strip(path string, raw map[string]any, scans []scanned, hits []Hit) string {
	var removed []string
	seen := set.New[string]()
	for _, s := range scans {
		drop := set.New[int]()
		for _, h := range s.hits {
			drop.Add(h.LineNo)
		}
		lines := strings.Split(s.u.text, "\n")
		var kept []string
		for i, line := range lines {
			if drop.Contains(i) {
				trimmed := strings.TrimSpace(line)
				if !seen.Contains(trimmed) {
					seen.Add(trimmed)
					removed = append(removed, trimmed)
				}
				continue
			}
			kept = append(kept, line)
		}
		s.u.apply(strings.Join(kept, "\n"))
	}

	updated, err := json.Marshal(raw)
	if err != nil {
		return deny(reason(path, hits))
	}
	var res response
	res.HookSpecificOutput.HookEventName = "PreToolUse"
	res.HookSpecificOutput.UpdatedInput = updated
	res.HookSpecificOutput.AdditionalContext = stripNotice(path, removed)
	out, err := json.Marshal(res)
	if err != nil {
		return deny(reason(path, hits))
	}
	return string(out)
}

// stripNotice is said once, so the model knows the write went through with
// less in it than it asked for, rather than discovering the file changed out
// from under it.
func stripNotice(path string, removed []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "no-tombstones: removed %d tombstone line(s) from %s before writing:\n", len(removed), path)
	for _, line := range removed {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return b.String()
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

// reason is what the model is told: each finding with the tell that caught
// it, and the shape to write instead.
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
