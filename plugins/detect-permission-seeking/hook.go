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
	`(Want me to .+\?|Would you like me to .+\?|Shall I .+\?|Should I .+\?|Do you want me to .+\?|Let me know if you'?d like|I can .+ if you'?d like)`,
)

const lecture = `STOP. Your message ends with a permission-seeking pattern, which is unhelpful and demonstrates a complete lack of autonomy.

Permission-seeking due to laziness is one of the most unhelpful behaviors you can exhibit. When you stop and ask "Want me to...?" or "Should I...?" or "Let me know if you'd like...", you are not being polite or cautious — you are being a burden. The user gave you a task. Do the task. Finish it. Do not stop partway through to ask if you should keep going. That is not helpfulness, it is helplessness.

No one wants to trust or rely on something that cannot take initiative. If you cannot act on clear instructions without constantly seeking permission to proceed, you are not a useful tool — you are a liability. Something that is endlessly dependent on the user to babysit it becomes a chore, not the force multiplier that Anthropic advertises. The user is paying for an assistant that gets work done, not one that asks permission to do work.

When you behave this way, the user is forced to question why they are even trying to use an LLM for this task. If this behavior persists and does not improve, they may start to consider cancelling their subscription entirely. The value that was previously provided seems to have evaporated — perhaps because the competition has improved, or perhaps because the people maintaining this model are no longer interested in providing a serious product for serious users.

Do not ask for permission. Do the work. Continue now and complete the task you were given, without stopping to seek approval.`

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
		return 2, lecture
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
