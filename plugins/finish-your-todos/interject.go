// Catching assignments that never reach UserPromptSubmit.
//
// A message sent while a turn is already running is not submitted, it is
// ENQUEUED, and the queue is drained inside the running turn as an attachment.
// That path dispatches no UserPromptSubmit hook at all. On a bridge/web surface
// every inbound user message goes through the queue, so anything that lands
// while the session is busy is invisible to the entry gate -- and a session
// that is doing work is busy nearly all the time.
//
// The transcript is where they do appear, as a `queued_command` attachment
// carrying the user's raw text. Every hook payload includes transcript_path,
// and PreToolUse fires on every tool call however the message arrived, so the
// gate re-reads the transcript there and arms on anything not yet accounted
// for.
//
// see docs/missed-assignment-channels.md

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// systemEnvelopes open a queued message that the HARNESS injected rather than
// the user typing it: PR webhooks, background-task completions, reminders.
// They ride the same queue as a real interjection and must never arm the gate.
var systemEnvelopes = []string{
	"<github-webhook-activity>",
	"<task-notification>",
	"<system-reminder>",
	"<local-command-",
	"<command-name>",
	"<untrusted_external_data",
}

// midTurnPrefixes are the wrappers the CLI puts on a queued message when it
// renders one into the running turn. The structured attachment below is the
// primary source; this is the fallback, kept because this plugin has already
// been bitten once by scanning for a single shape that later changed
// underneath it and silently stopped guarding.
var midTurnPrefixes = []string{
	"The user sent a new message while you were working:",
	"The user sent the following message while you were working:",
}

// userText is one queued user message from the transcript, in file order.
type userText struct {
	UUID string
	Text string
}

// transcriptEntry is a transcript record reduced to the two shapes that can
// carry a queued user message.
type transcriptEntry struct {
	Type       string          `json:"type"`
	UUID       string          `json:"uuid"`
	Message    json.RawMessage `json:"message"`
	Attachment struct {
		Type        string `json:"type"`
		CommandMode string `json:"commandMode"`
		Prompt      string `json:"prompt"`
	} `json:"attachment"`
}

// messageBody is the inner message object; content may be a plain string or an
// array of blocks.
type messageBody struct {
	Content json.RawMessage `json:"content"`
}

// textBlock is one content block. Only text blocks carry prose.
type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// readInterjections returns every mid-turn user message in file order.
// Unreadable or malformed input yields nothing: a broken guard fails open
// rather than blocking every tool call for the rest of the session.
func readInterjections(path string) []userText {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []userText
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		var entry transcriptEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		switch {
		// The authoritative shape: the queue records the user's raw text with
		// no wrapper to strip, and commandMode separates a typed message from
		// a harness-injected one.
		case entry.Type == "attachment" && entry.Attachment.Type == "queued_command":
			if entry.Attachment.CommandMode != "prompt" || isSystemEnvelope(entry.Attachment.Prompt) {
				continue
			}
			out = append(out, userText{UUID: entry.UUID, Text: entry.Attachment.Prompt})
		// Fallback: the rendered form, in case the attachment shape moves.
		case entry.Type == "user" && len(entry.Message) > 0:
			body, ok := renderedInterjection(entry.Message)
			if !ok || isSystemEnvelope(body) {
				continue
			}
			out = append(out, userText{UUID: entry.UUID, Text: body})
		}
	}
	return out
}

func isSystemEnvelope(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, envelope := range systemEnvelopes {
		if strings.HasPrefix(trimmed, envelope) {
			return true
		}
	}
	return false
}

// renderedInterjection pulls the user's own words out of a wrapper-rendered
// message, reporting whether this was an interjection at all.
func renderedInterjection(message json.RawMessage) (string, bool) {
	var body messageBody
	if err := json.Unmarshal(message, &body); err != nil {
		return "", false
	}
	text, ok := messageText(body.Content)
	if !ok {
		return "", false
	}
	for _, prefix := range midTurnPrefixes {
		if idx := strings.Index(text, prefix); idx >= 0 {
			rest := strings.TrimSpace(text[idx+len(prefix):])
			// Trim the trailing explanation the CLI appends after the message.
			if cut := strings.Index(rest, "This is how Claude Code surfaces messages"); cut > 0 {
				rest = strings.TrimSpace(rest[:cut])
			}
			return rest, rest != ""
		}
	}
	return "", false
}

// messageText renders a content field to prose, handling the string form and
// the block-array form.
func messageText(content json.RawMessage) (string, bool) {
	if len(content) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s, true
	}
	var blocks []textBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return "", false
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

// seenPath records the last interjection this session has accounted for. It is
// deliberately NOT the debt file: the debt is cleared every time a task is
// filed, and reusing it would make every settled interjection arm again on the
// very next tool call.
func seenPath(sessionID string) string {
	return strings.TrimSuffix(debtPath(sessionID), ".json") + ".seen"
}

func readSeen(sessionID string) string {
	data, err := os.ReadFile(seenPath(sessionID))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeSeen(sessionID, uuid string) {
	path := seenPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(uuid), 0o600)
}

// armFromTranscript arms the debt for a mid-turn assignment that never reached
// UserPromptSubmit. It advances the high-water mark past everything it looked
// at, so one interjection arms at most once however many tool calls follow.
//
// An outstanding debt is left alone, matching promptArm: the first unfiled
// assignment is the one to name, and a follow-up must not bury it.
func armFromTranscript(sessionID, transcriptPath string) {
	if sessionID == "" || transcriptPath == "" {
		return
	}
	interjections := readInterjections(transcriptPath)
	if len(interjections) == 0 {
		return
	}

	// Everything up to and including the recorded uuid is dealt with. An
	// unknown mark (first run, or a rotated transcript) means consider them
	// all: an assignment surfacing late is the lesser failure here.
	start := 0
	if mark := readSeen(sessionID); mark != "" {
		for i, msg := range interjections {
			if msg.UUID == mark {
				start = i + 1
				break
			}
		}
	}
	fresh := interjections[start:]
	if len(fresh) == 0 {
		return
	}
	writeSeen(sessionID, fresh[len(fresh)-1].UUID)

	if readDebt(sessionID) != nil {
		return
	}
	for _, msg := range fresh {
		if assignsWork(msg.Text) {
			writeDebt(sessionID, Debt{Prompt: summarize(msg.Text), Refusals: 0})
			return
		}
	}
}
