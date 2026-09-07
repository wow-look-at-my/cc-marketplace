// transcript.go answers one question: did this turn ask through the
// AskUserQuestion tool? The message being judged arrives on the hook payload
// itself, flush by flush, so nothing is read out of the transcript but this.
//
// Only THIS turn's tool calls count. A question asked properly three turns ago
// does not license one in prose now.
package main

import (
	"bytes"
	"encoding/json"
	"os"
)

// transcriptTailBytes bounds the read. A long session's transcript reaches
// hundreds of megabytes, and only the last turn matters.
const transcriptTailBytes = 4 << 20 // 4 MiB

// askTool is the tool that asks a question the right way: a rendered card the
// user answers by selection, never prose the user has to reply to.
const askTool = "AskUserQuestion"

type transcriptRecord struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"`
}

// Turn is what one invocation reads out of the transcript.
type Turn struct {
	UsedAskTool bool
}

// ReadTurn reports whether the current turn called AskUserQuestion. An
// unreadable or empty transcript returns a zero Turn, which leaves the message
// judged on its own text -- the worst that costs is one advisory line under a
// message that had a card beside it.
func ReadTurn(path string) Turn {
	if path == "" {
		return Turn{}
	}
	lines, err := readTail(path)
	if err != nil {
		return Turn{}
	}

	recs := make([]transcriptRecord, 0, len(lines))
	for _, line := range lines {
		var rec transcriptRecord
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		recs = append(recs, rec)
	}

	start := turnStart(recs)

	var turn Turn
	for i := len(recs) - 1; i >= start; i-- {
		if recs[i].Message.Role != "assistant" {
			continue
		}
		var blocks []contentBlock
		if json.Unmarshal(recs[i].Message.Content, &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "tool_use" && b.Name == askTool {
				turn.UsedAskTool = true
			}
		}
	}
	return turn
}

// turnStart returns the index of the record that begins the current turn: the
// last real user prompt. A user record carrying only tool_result blocks
// answers a call from earlier in the SAME turn and must not split it -- the
// same boundary rule the sibling no-busy-poll plugin uses.
func turnStart(recs []transcriptRecord) int {
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Message.Role != "user" && recs[i].Type != "user" {
			continue
		}
		if isNewPrompt(recs[i]) {
			return i
		}
	}
	return 0
}

// isNewPrompt reports whether a user record starts a turn rather than
// continuing one. A plain string content is always a prompt; an array is a
// prompt unless every block in it is a tool_result.
func isNewPrompt(rec transcriptRecord) bool {
	var blocks []contentBlock
	if json.Unmarshal(rec.Message.Content, &blocks) != nil {
		return true
	}
	if len(blocks) == 0 {
		return true
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			return true
		}
	}
	return false
}

// readTail returns the JSONL lines from the last transcriptTailBytes of the
// file. When the file is longer than the window the first line is dropped, so
// no half record is parsed.
func readTail(path string) ([][]byte, error) {
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
