package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
)

type HookInput struct {
	HookEventName        string `json:"hook_event_name"`
	TranscriptPath       string `json:"transcript_path"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

type TranscriptEntry struct {
	Role    string `json:"role"`
	Message string `json:"message"`
	Content string `json:"content"`
}

var permissionPattern = regexp.MustCompile(
	`(Want me to .+\?|Would you like me to .+\?|Shall I .+\?|Should I .+\?|Do you want me to .+\?|Let me know if you'?d like|I can .+ if you'?d like|Say the word)`,
)

// resume is the whole refusal. It is what the user would have typed back.
//
// This has to stay a Stop hook, unlike the guards that judge a message for its
// wording. Those annotate what the reader sees, because the message has already
// streamed and refusing cannot unsend it. This one exists to stop the model
// STOPPING, and only a Stop hook can do that. An annotation under an abandoned
// turn changes nothing about the turn being abandoned.
//
// One word, because the refusal is read by the thing that just asked to stop.
// The lecture this replaced ran four paragraphs and argued its own case, which
// gave the model a case to argue back with and spent the turn on that instead
// of the work. There is nothing here to reply to.
const resume = `continue`

// getLastAssistantMessage extracts the assistant's last message from the hook input.
// Primary: the last_assistant_message field.
// Fallback: read the transcript JSONL file.
func getLastAssistantMessage(hi HookInput) string {
	if hi.LastAssistantMessage != "" {
		return hi.LastAssistantMessage
	}

	if hi.TranscriptPath == "" {
		return ""
	}

	f, err := os.Open(hi.TranscriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var lastMsg string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		var entry TranscriptEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Role == "assistant" {
			msg := entry.Message
			if msg == "" {
				msg = entry.Content
			}
			if msg != "" {
				lastMsg = msg
			}
		}
	}
	return lastMsg
}

// evaluate checks whether the assistant's last message contains permission-seeking patterns.
// Returns exit code (0 = allow stop, 2 = block stop) and a message for stderr when blocking.
func evaluate(input []byte) (int, string) {
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		return 0, ""
	}

	if hi.HookEventName != "Stop" {
		return 0, ""
	}

	msg := getLastAssistantMessage(hi)
	if msg == "" {
		return 0, ""
	}

	if permissionPattern.MatchString(msg) {
		return 2, resume
	}

	return 0, ""
}

// run reads stdin and returns the exit code and stderr message.
func run(r io.Reader) (int, string) {
	input, _ := io.ReadAll(r)
	return evaluate(input)
}

func main() {
	code, msg := run(os.Stdin)
	if msg != "" {
		fmt.Fprint(os.Stderr, msg)
	}
	os.Exit(code)
}
