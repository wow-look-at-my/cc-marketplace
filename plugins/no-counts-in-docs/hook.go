// Command no-counts-in-docs is a Claude Code PreToolUse hook that cuts a count
// out of a write to a markdown document, then lets the write through.
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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// slopfmtBinary is the tool that owns the rule. This plugin holds no copy of
// it: CI, the editor and this hook all shell out to the same binary, so none of
// them can drift from the others. SLOPFMT names another path.
var slopfmtBinary = envOr("SLOPFMT", "slopfmt")

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// Hit is one count slopfmt cut, as this hook reports it back to the model.
type Hit struct {
	Phrase string
}

// IsMarkdown reports whether a path names a document this plugin governs. The
// RULE lives in slopfmt. This only decides which writes are worth a subprocess.
func IsMarkdown(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}

// countsRepair is what `slopfmt fix --only counts --json` answers with.
type countsRepair struct {
	Text    string   `json:"text"`
	Changed bool     `json:"changed"`
	Removed []string `json:"removed"`
}

// strip runs the text through slopfmt. A missing binary, a timeout and an
// unreadable answer all return the text unchanged: a guard that mangles a write
// because its tool is absent is worse than no guard.
func strip(text string) (countsRepair, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, slopfmtBinary, "fix", "--only", "counts", "--json")
	command.Stdin = strings.NewReader(text)
	var out bytes.Buffer
	command.Stdout = &out
	if err := command.Run(); err != nil {
		return countsRepair{}, false
	}
	var repair countsRepair
	if json.Unmarshal(out.Bytes(), &repair) != nil {
		return countsRepair{}, false
	}
	return repair, repair.Changed
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

// response is the repair payload: the same write with the numbers cut out, and
// one line naming what went. There is no permissionDecision, so the normal
// permission flow still runs on the repaired write.
type response struct {
	HookSpecificOutput struct {
		HookEventName     string         `json:"hookEventName"`
		UpdatedInput      map[string]any `json:"updatedInput"`
		AdditionalContext string         `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func main() {
	out := run(os.Stdin)
	if out != "" {
		fmt.Print(out)
	}
}

// run reads a hook payload from r and returns the JSON to print, or "" to let
// the call through unchanged. Every failure path returns "": a guard that
// mangles a write it could not parse is worse than no guard.
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
	var raw map[string]any
	if json.Unmarshal(in.ToolInput, &raw) != nil {
		return ""
	}
	hits := repair(raw)
	if len(hits) == 0 {
		return ""
	}
	return mutate(raw, ti.FilePath, hits)
}

// repair strips the counts out of every text this write puts into the file,
// editing raw in place, and returns the hits it acted on. Unknown keys survive
// because the whole payload is carried through as it arrived.
func repair(raw map[string]any) []Hit {
	var hits []Hit
	fix := func(m map[string]any, key string) {
		text, ok := m[key].(string)
		if !ok {
			return
		}
		result, changed := strip(text)
		if !changed {
			return
		}
		m[key] = result.Text
		for _, phrase := range result.Removed {
			hits = append(hits, Hit{Phrase: phrase})
		}
	}
	fix(raw, "content")
	fix(raw, "new_string")
	if edits, ok := raw["edits"].([]any); ok {
		for _, e := range edits {
			if m, ok := e.(map[string]any); ok {
				fix(m, "new_string")
			}
		}
	}
	return hits
}

func mutate(input map[string]any, path string, hits []Hit) string {
	var res response
	res.HookSpecificOutput.HookEventName = "PreToolUse"
	res.HookSpecificOutput.UpdatedInput = input
	res.HookSpecificOutput.AdditionalContext = notice(path, hits)
	out, err := json.Marshal(res)
	if err != nil {
		return ""
	}
	return string(out)
}

// notice is what the model is told after the fact. The write went through, so
// this names the numbers that were cut and why, rather than asking for a retry.
func notice(path string, hits []Hit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "no-counts-in-docs removed a count from this write to %s:\n", path)
	for _, hit := range hits[:min(len(hits), 6)] {
		fmt.Fprintf(&b, "  %q\n", hit.Phrase)
	}
	b.WriteString(remedy)
	return b.String()
}

// remedy is the standing half of the notice: why a count rots, and the shape to
// write instead.
const remedy = `
A count is true only until somebody adds or removes an item, and nothing in the
repository corrects it when they do -- the reader keeps trusting a number that
has quietly gone wrong. Describe what is there and let the reader count:
"every plugin this repo installs", not "this repo's 15 plugins"; "the rules
below", not "the four rules below".

The number is already gone from the text that was written. Read the sentence
back if it now needs rewording.`
