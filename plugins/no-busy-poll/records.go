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
// id ties it to its result, so a call that came back an error is not counted
// as a read.
type toolCall struct {
	id    string
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
	// failed names the tool_use ids whose result came back an error. A call
	// that errored returned no state, so re-asking is the FIRST read of that
	// subject rather than a repeat of one.
	failed []string
	// answered names every tool_use id a result has arrived for, error or
	// not. A call with NO result carries no state either: it is in flight, or
	// its result is not on disk yet when the next call's hook reads the
	// transcript. Counting one as a read refuses the retry of a call that
	// just failed.
	answered []string
	raw      string
}

// wakeMarkers are the envelopes the harness delivers when something really
// happened. Each one is new information, so each one re-opens every subject.
var wakeMarkers = []string{
	"<wake reason=",
	"<task-notification>",
	"<webhook-payload>",
	"<event source=",
}

// interjectionMarker is how a message the user types MID-TURN reaches the
// transcript: not as a record of its own, but folded into the content of the
// tool_result that happened to come back next. Its blocks are therefore all
// tool_result, which is exactly the shape isNewPrompt reads as "no prompt
// here". The user telling a session its pull request is red then counted as
// nothing, and the next read of that pull request was refused as a repeat.
const interjectionMarker = "the user sent a new message while you were working"

// parseRecords reads the tail of the transcript at path, keeping only what
// THIS session did. An unreadable or empty transcript returns nil, which
// allows every call: a guard that blocks because it could not read a file is
// worse than no guard.
//
// Two kinds of record are dropped, and both were seeding the guard with reads
// this session never made. A resumed or imported conversation writes its
// earlier records into the same file, each still carrying the session id that
// produced it, so a brand-new session inherited a full ledger of pull requests
// somebody else had already read: `gh pr view 130` was refused as a repeat on
// its FIRST call. A subagent's records land in the same file too, marked
// isSidechain, and a read the subagent made is not a read the caller has the
// answer to. sessionID is compared only when both sides carry one, so a
// transcript shape without the field behaves as it always did.
func parseRecords(path, sessionID string) []record {
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
		if rec.Sidechain {
			continue
		}
		if sessionID != "" && rec.SessionID != "" && rec.SessionID != sessionID {
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
			r.newPrompt = isNewPrompt(rec.Message.Content) || strings.Contains(lower, interjectionMarker)
			var blocks []rawBlock
			if json.Unmarshal(rec.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					if b.Type != "tool_result" || b.ToolUseID == "" {
						continue
					}
					r.answered = append(r.answered, b.ToolUseID)
					if b.IsError {
						r.failed = append(r.failed, b.ToolUseID)
					}
				}
			}
		case "assistant":
			var blocks []rawBlock
			if json.Unmarshal(rec.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					if b.Type == "tool_use" {
						r.calls = append(r.calls, toolCall{id: b.ID, name: b.Name, input: b.Input})
					}
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// callText is the text a subject is read out of. For an MCP tool that is the
// whole input, whose fields ARE the question. A Bash call is the exception:
// its input carries a `description` written for a human, and a subject read
// out of that is a subject the command never asked about.
//
// Measured live. `gh wait-ci runs --branch <b>` described as "Find the run
// for 58e180d" was refused as a repeat read of 58e180d, a commit its command
// does not name. The same defect marks a subject READ from a description, so
// the first genuine read of it is then refused as a repeat.
// A Bash call narrows further still: only the status commands it runs, each
// bounded to its own statement. `git show <sha>:src/cmd/go.mod` names a commit
// and reads a local object, and a SHA in one statement says nothing about what
// the `gh` call in the next one is asking after.
func callText(c toolCall) string {
	if strings.EqualFold(c.name, "bash") {
		return c.name + " " + strings.Join(statusStatements(commandOf(c.input)), " ; ")
	}
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
