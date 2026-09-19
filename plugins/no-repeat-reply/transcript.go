// transcript.go answers a single question: what did each turn CLOSE with?
//
// A turn's closing message is an assistant record whose stop_reason is
// "end_turn" and which carries a text block. A record that stopped with
// tool_use is mid-turn, and a thinking block is not a message to the user.
// Comparing consecutive closing messages is what detects the livelock.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// transcriptTailBytes bounds the read. Only the last few turns decide
// anything, so a long session's transcript never has to be read whole.
const transcriptTailBytes = 4 << 20

type transcriptRecord struct {
	Type    string `json:"type"`
	Message struct {
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// closingTexts returns the closing message of each turn, in transcript
// order. An unreadable transcript returns nothing, which allows the stop.
func closingTexts(path string) []string {
	lines, err := readTranscriptTail(path)
	if err != nil {
		return nil
	}
	var texts []string
	for _, line := range lines {
		var rec transcriptRecord
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		if rec.Type != "assistant" || rec.Message.StopReason != "end_turn" {
			continue
		}
		if text := messageText(rec.Message.Content); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

// readTranscriptTail returns the JSONL lines from the last
// transcriptTailBytes of the file. A truncated leading line is dropped when
// the file is longer than the window, so no half record is parsed.
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

// messageText joins the text blocks of a single assistant message.
// Thinking and tool_use blocks are not part of what the user read.
func messageText(raw json.RawMessage) string {
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
