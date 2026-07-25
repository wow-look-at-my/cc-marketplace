// transcript.go answers the one question the PreToolUse gate needs: has the
// assistant already replied to the user in this turn? Without it the block
// could only be lifted by the Stop event, which made the guard turn-scoped:
// a question whose answer requires a tool ("is this all committed?" ->
// `git status`) could not be answered at all, because the model had to end
// its turn to regain tools, and ending the turn hands control back to the
// user. The model then either guessed or stalled for a filler message. With
// this check the block is text-scoped instead: reply, then act, in the same
// turn.
//
// Three facts about Claude Code's transcript make this reliable (verified
// against a live transcript and the CLI source, 2.1.x):
//
//  1. PreToolUse receives transcript_path -- its payload spreads the same
//     base builder as every other hook event (cli.js: PreToolUse payload ->
//     `{...Am(...), hook_event_name: "PreToolUse", tool_name, tool_input,
//     tool_use_id}`, and Am supplies session_id/transcript_path/cwd/...).
//  2. Every content block is its own JSONL record, appended in order, and
//     an assistant text record is flushed BEFORE the tool_use record that
//     follows it (observed gap: text at 05:11:59.754Z, the tool_use it
//     preceded at 05:12:02.111Z). So by the time this hook runs, the
//     reply -- if there is one -- is already on disk.
//  3. A real user prompt is distinguishable from a tool result: prompts
//     carry string content (or an array with no tool_result block, e.g.
//     with attachments), tool results carry an array of tool_result blocks.
//     The two never mix in one record.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// transcriptTailBytes bounds how much of the transcript the reply check
// reads. Only the current turn is relevant, so reading a bounded tail keeps
// a hook that fires on every tool call cheap even when a long session's
// transcript has grown to hundreds of megabytes.
const transcriptTailBytes = 4 << 20 // 4 MiB

// transcriptRecord is one JSONL line. Content is raw because it is a string
// for typed prompts and an array of blocks everywhere else.
type transcriptRecord struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentBlock is one block inside a message's content array.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// hasRepliedSince reports whether the assistant has emitted a non-empty
// text block since the last real user prompt in the transcript at path.
//
// It scans records from the end backwards and stops at the first decisive
// one: an assistant text block means the reply is out (true); a real user
// prompt means we reached the start of this turn without finding one
// (false). Tool calls, tool results, thinking blocks and attachments are
// skipped -- none of them is a reply to the user.
//
// An unreadable or empty transcript returns false, which keeps the block
// armed. That is the safe direction: it degrades to the original
// turn-scoped behavior (end the turn and Stop clears the block) rather than
// silently turning the guard off, and it cannot wedge a session.
func hasRepliedSince(path string) bool {
	if path == "" {
		return false
	}
	lines, err := readTranscriptTail(path)
	if err != nil {
		return false
	}
	for i := len(lines) - 1; i >= 0; i-- {
		var rec transcriptRecord
		if json.Unmarshal(lines[i], &rec) != nil {
			continue
		}
		switch rec.Type {
		case "assistant":
			if assistantEmittedText(rec.Message.Content) {
				return true
			}
		case "user":
			if isUserPrompt(rec.Message.Content) {
				return false
			}
		}
	}
	return false
}

// readTranscriptTail returns the JSONL lines from the last
// transcriptTailBytes of the file. When the file is longer than the window
// the first (possibly partial) line is dropped so no half record is parsed.
func readTranscriptTail(path string) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := int64(0)
	if st.Size() > transcriptTailBytes {
		offset = st.Size() - transcriptTailBytes
	}
	buf := make([]byte, st.Size()-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && len(buf) == 0 {
		return nil, err
	}

	lines := bytes.Split(buf, []byte("\n"))
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	return lines, nil
}

// assistantEmittedText reports whether an assistant message's content holds
// a text block with actual content. Thinking blocks and tool_use blocks do
// not count: neither is a reply the user can read as an answer.
func assistantEmittedText(raw json.RawMessage) bool {
	for _, b := range parseBlocks(raw) {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}

// isUserPrompt reports whether a user record is a real prompt from the human
// rather than a tool result being fed back. String content is always a typed
// prompt; an array is a prompt only if it carries no tool_result block (an
// attachment-bearing prompt is still a prompt).
func isUserPrompt(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return true
	}
	blocks := parseBlocks(raw)
	if len(blocks) == 0 {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return false
		}
	}
	return true
}

func parseBlocks(raw json.RawMessage) []contentBlock {
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	return blocks
}
