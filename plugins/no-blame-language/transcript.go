// transcript.go pulls the one thing this plugin judges: the text the assistant
// just put in front of the user.
//
// Only the closing message counts. A phrase that appeared in a tool result, a
// thinking block, or three turns ago is not the message the reader is being
// asked to accept right now.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// transcriptTailBytes bounds the read. A long session's transcript reaches
// hundreds of megabytes, and only the last message matters.
const transcriptTailBytes = 4 << 20 // 4 MiB

// transcriptRecord is one JSONL line, reduced to the role and content this
// plugin reads.
type transcriptRecord struct {
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentBlock is one block inside a message's content array.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// FinalAssistantText returns the text blocks of the last assistant message in
// the transcript at path, joined in order. An unreadable, empty, or
// text-less transcript returns "" -- which allows the stop, because a guard
// that blocks on its own read failure wedges the session it was meant to
// improve.
func FinalAssistantText(path string) string {
	if path == "" {
		return ""
	}
	lines, err := readTail(path)
	if err != nil {
		return ""
	}
	for i := len(lines) - 1; i >= 0; i-- {
		var rec transcriptRecord
		if json.Unmarshal(lines[i], &rec) != nil {
			continue
		}
		if rec.Message.Role != "assistant" {
			continue
		}
		var blocks []contentBlock
		if json.Unmarshal(rec.Message.Content, &blocks) != nil {
			continue
		}
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
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
