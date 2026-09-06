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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/wow-look-at-my/go-containers/set"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// slopfmtPath names the tool that owns the rule. This plugin holds no copy of
// it: CI, the editor and this hook all shell out to the same binary, so none of
// them can drift from the others. SLOPFMT names another path.
//
// It reads the environment per call rather than once. A package-level variable
// is shared state, and the tests that point it elsewhere run in parallel.
func slopfmtPath() string { return envOr("SLOPFMT", "slopfmt") }

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// Hit is a tombstone slopfmt reported and no deletion resolved.
type Hit struct {
	Tell   string `json:"tell"`
	Phrase string `json:"phrase"`
	Line   string `json:"line"`
}

// repair is what `slopfmt fix --only tombstones --json` answers with.
type repair struct {
	Text    string   `json:"text"`
	Changed bool     `json:"changed"`
	Removed []string `json:"removed"`
	Kept    []Hit    `json:"kept"`
}

// scan puts the text a write adds to slopfmt. A missing binary, a timeout and
// an unreadable answer all report nothing, because a guard that refuses a write
// when its tool is absent is worse than no guard.
func scan(path, text string, maxLines int) (repair, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, slopfmtPath(),
		"fix", "--only", "tombstones", "--json",
		"--path", path, "--max-comment-lines", strconv.Itoa(maxLines))
	command.Stdin = strings.NewReader(text)
	var out bytes.Buffer
	command.Stdout = &out
	// A finding exits non-zero, so the answer is read before the exit code.
	_ = command.Run()
	var answer repair
	if json.Unmarshal(out.Bytes(), &answer) != nil {
		return repair{}, false
	}
	return answer, true
}

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

	var removed []string
	var kept []Hit
	seen := set.New[string]()
	for _, u := range writeUnits(in.ToolName, ti, raw) {
		answer, ok := scan(ti.FilePath, u.text, maxCommentLines())
		if !ok {
			continue
		}
		kept = append(kept, answer.Kept...)
		for _, line := range answer.Removed {
			if !seen.Contains(line) {
				seen.Add(line)
				removed = append(removed, line)
			}
		}
		if answer.Changed {
			u.apply(answer.Text)
		}
	}
	// A finding no deletion resolved refuses the whole write, because a strip
	// that guesses at a span corrupts the file worse than a round trip does.
	if len(kept) > 0 {
		return deny(reason(ti.FilePath, kept))
	}
	if len(removed) == 0 {
		return ""
	}
	return strip(ti.FilePath, raw, removed)
}

// strip returns the allow-with-updatedInput response. Every unit's text has
// already been replaced with what slopfmt handed back.
func strip(path string, raw map[string]any, removed []string) string {
	updated, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	var res response
	res.HookSpecificOutput.HookEventName = "PreToolUse"
	res.HookSpecificOutput.UpdatedInput = updated
	res.HookSpecificOutput.AdditionalContext = stripNotice(path, removed)
	out, err := json.Marshal(res)
	if err != nil {
		return ""
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
