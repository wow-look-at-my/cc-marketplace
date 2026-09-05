// records.go is the raw view of the transcript the PreToolUse half needs.
// The Stop half works from turns, which keep only call signatures; deciding
// whether a status read can still learn anything needs the RESULT text too,
// because that is where a merge or a green build is reported.
package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

// toolCall is one tool_use block: the tool it names and the input it carries.
type toolCall struct {
	name  string
	input json.RawMessage
}

// record is one JSONL line, reduced to what the decision reads. raw is kept
// whole because a terminal verdict arrives inside a tool result whose shape
// differs per tool, and matching text is the only thing that covers them all.
type record struct {
	newPrompt bool
	wake      bool
	calls     []toolCall
	raw       string
}

// wakeMarkers are the envelopes the harness delivers when something really
// happened. Each one is new information, so each one re-opens every subject.
var wakeMarkers = []string{
	"<wake reason=",
	"<task-notification>",
	"<webhook-payload>",
	"<event source=",
}

// parseRecords reads the tail of the transcript at path. An unreadable or
// empty transcript returns nil, which allows every call: a guard that blocks
// because it could not read a file is worse than no guard.
func parseRecords(path string) []record {
	if path == "" {
		return nil
	}
	lines, err := readTail(path, transcriptTailBytes)
	if err != nil {
		return nil
	}

	var out []record
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec rawRecord
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		r := record{raw: unescape(string(line))}
		lower := strings.ToLower(r.raw)
		for _, m := range wakeMarkers {
			if strings.Contains(lower, m) {
				r.wake = true
				break
			}
		}
		switch rec.Type {
		case "user":
			r.newPrompt = isNewPrompt(rec.Message.Content)
		case "assistant":
			var blocks []rawBlock
			if json.Unmarshal(rec.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					if b.Type == "tool_use" {
						r.calls = append(r.calls, toolCall{name: b.Name, input: b.Input})
					}
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// callText is the text a subject is read out of: the tool name plus its
// input. One string means one extractor serves a Bash command line and an
// MCP tool's JSON arguments alike.
func callText(c toolCall) string {
	return c.name + " " + string(c.input)
}

// unescapeReplacer undoes one level of JSON string escaping. The \uXXXX
// entries cover the three characters a Go encoder escapes by default and a
// JavaScript one leaves alone: a transcript written by either must read the
// same, or a wake envelope is invisible on one of them and the guard fails
// open without saying so.
var unescapeReplacer = strings.NewReplacer(
	`\"`, `"`,
	`\\`, `\`,
	`\n`, "\n",
	"\\u003c", "<", "\\u003C", "<",
	"\\u003e", ">", "\\u003E", ">",
	"\\u0026", "&",
)

// unescape flattens a record so a verdict inside a tool result is findable.
// A result's payload is a JSON string nested in the record's own JSON, so
// `{"outcome":"merged"}` reaches the transcript as `{\"outcome\":\"merged\"}`
// and matching the unescaped spelling finds nothing at all. This is text
// matching, not parsing: one pass makes every nesting depth's quotes plain.
func unescape(line string) string {
	return unescapeReplacer.Replace(line)
}
