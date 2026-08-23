// transcript.go turns the flat JSONL transcript into an ordered list of
// turns, where a turn is everything the assistant did between one real user
// prompt and the next. A tool_result record answers a call made earlier in
// the SAME turn, so it never starts a new one; only a genuine new prompt
// does -- a real user message, or the text a Stop hook injects on refusal.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

// transcriptTailBytes bounds the read. A long session's transcript reaches
// hundreds of megabytes, and only the last several turns matter here.
const transcriptTailBytes = 6 << 20 // 6 MiB

// rawRecord is one JSONL line, reduced to the fields a turn needs.
type rawRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// rawBlock is one block inside a message's content array.
type rawBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// call is one tool_use block, kept as both a canonical form for comparing
// turns and a human rendering for the refusal message.
type call struct {
	canon string // "Name|<canonical JSON input>", used for equality
	disp  string // "Name: <readable input>", used in the message
}

// turn is everything the assistant did between two real prompts: which
// tools it called (sig, for comparing turns; calls, for display) and when
// it started and finished, so an idle gap before it can be measured.
type turn struct {
	calls     []call
	sig       string
	startedAt time.Time
	endedAt   time.Time
}

// parseTurns reads the tail of the transcript at path and segments it into
// turns. An unreadable or empty transcript returns nil, which never
// triggers a refusal -- a guard that blocks on its own read failure is
// worse than no guard.
func parseTurns(path string) []turn {
	if path == "" {
		return nil
	}
	lines, err := readTail(path, transcriptTailBytes)
	if err != nil {
		return nil
	}

	var turns []turn
	var cur *turn
	var curCalls []call

	closeCurrent := func() {
		if cur == nil {
			return
		}
		sort.Slice(curCalls, func(i, j int) bool { return curCalls[i].canon < curCalls[j].canon })
		var canon []string
		for _, c := range curCalls {
			canon = append(canon, c.canon)
		}
		cur.calls = curCalls
		cur.sig = strings.Join(canon, "\n")
		turns = append(turns, *cur)
	}

	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec rawRecord
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		ts, tsErr := time.Parse(time.RFC3339, rec.Timestamp)

		switch rec.Type {
		case "user":
			if isNewPrompt(rec.Message.Content) {
				closeCurrent()
				cur = &turn{}
				curCalls = nil
				if tsErr == nil {
					cur.startedAt, cur.endedAt = ts, ts
				}
				continue
			}
			if cur == nil {
				cur = &turn{}
				curCalls = nil
				if tsErr == nil {
					cur.startedAt, cur.endedAt = ts, ts
				}
			} else if tsErr == nil {
				cur.endedAt = ts
			}
		case "assistant":
			if cur == nil {
				cur = &turn{}
				curCalls = nil
				if tsErr == nil {
					cur.startedAt, cur.endedAt = ts, ts
				}
			} else if tsErr == nil {
				cur.endedAt = ts
			}
			var blocks []rawBlock
			if json.Unmarshal(rec.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					if b.Type == "tool_use" {
						curCalls = append(curCalls, call{
							canon: b.Name + "|" + canonicalJSON(b.Input),
							disp:  renderCall(b.Name, b.Input),
						})
					}
				}
			}
		default:
			if cur != nil && tsErr == nil {
				cur.endedAt = ts
			}
		}
	}
	closeCurrent()
	return turns
}

// isNewPrompt reports whether a "user"-role record is a genuine new prompt
// (real user text, or a Stop hook's injected refusal reason) rather than a
// tool_result answering a call earlier in the same turn. A record whose
// content is not a parseable block array -- a plain string, the shape a
// simple user message can take -- counts as a genuine prompt too.
func isNewPrompt(content json.RawMessage) bool {
	var blocks []rawBlock
	if json.Unmarshal(content, &blocks) != nil {
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

// canonicalJSON re-marshals input with sorted object keys, so two calls
// built from the same field values compare equal regardless of the order a
// map happened to iterate in when they were first encoded.
func canonicalJSON(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(input, &v) != nil {
		return string(input)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(input)
	}
	return string(out)
}

// renderCall is the one-line, human-readable form of a tool call for the
// refusal message. Bash shows its command directly, since that is the
// shape a busy-poll almost always takes; anything else shows compact JSON.
func renderCall(name string, input json.RawMessage) string {
	if name == "Bash" {
		var b struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(input, &b) == nil && b.Command != "" {
			return "Bash: " + oneLine(b.Command, 200)
		}
	}
	var v any
	if json.Unmarshal(input, &v) == nil {
		if compact, err := json.Marshal(v); err == nil {
			return name + ": " + oneLine(string(compact), 200)
		}
	}
	return name
}

// oneLine collapses internal whitespace and truncates to n characters, so a
// multi-line command or a large JSON blob still fits one message line.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// readTail returns the JSONL lines from the last maxBytes of the file at
// path. When the file is longer than the window the first line is dropped,
// so no half record is parsed.
func readTail(path string, maxBytes int64) ([][]byte, error) {
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
	if st.Size() > maxBytes {
		offset = st.Size() - maxBytes
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
