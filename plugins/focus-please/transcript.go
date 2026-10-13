// transcript.go answers the question the PreToolUse gate needs: has the
// assistant already replied to the user in this turn? Without it the block
// could only be lifted by the Stop event, which made the guard turn-scoped. A
// question whose answer requires a tool ("is this all committed?" -> `git
// status`) could not be answered at all. This is because the model had to end
// its turn to regain tools, and ending the turn hands control back to the
// user. The model then either guessed or stalled for a filler message. With
// this check the block is text-scoped instead: reply, then act, in the same
// turn.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// transcriptTailBytes bounds how much of the transcript the reply check reads.
const transcriptTailBytes = 4 << 20

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

// hasRepliedSince reports whether the assistant has emitted a non-empty text
// block since the last real user prompt in the transcript at path.
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

// assistantEmittedText reports whether an assistant message's content holds a
// text block with actual content.
func assistantEmittedText(raw json.RawMessage) bool {
	for _, b := range parseBlocks(raw) {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}

// isUserPrompt reports whether a user record is a real prompt from the human
// rather than a tool result being fed back.
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
