// Command docs-nudge is a PreToolUse hook that pulls this plugin's Docker
// skills in when a tool call is about to touch a Dockerfile or a Compose file.
//
// The skills already carry trigger-shaped descriptions and still do not get
// loaded reliably: a description competes for attention with every other
// description, and it is consulted when the model decides to look for a skill,
// which is exactly the decision that gets skipped. This hook fires on the tool
// call itself, so the reminder arrives at the moment the wrong content is about
// to be written.
//
// It never denies anything. The output is `hookSpecificOutput.additionalContext`,
// which Claude Code delivers to the model as a message; a denial here would
// block ordinary work over a documentation reminder.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type toolInput struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
}

type payload struct {
	HookEventName string    `json:"hook_event_name"`
	SessionID     string    `json:"session_id"`
	ToolName      string    `json:"tool_name"`
	ToolInput     toolInput `json:"tool_input"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

type output struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

func main() {
	// Every failure path stays silent and allows the call. This hook adds a
	// reminder; it must never be the reason a session cannot do its work.
	defer func() { _ = recover() }()

	var p payload
	if err := json.NewDecoder(os.Stdin).Decode(&p); err != nil {
		return
	}
	if p.HookEventName != "PreToolUse" {
		return
	}

	topics := topicFor(p.ToolName, p.ToolInput)
	if len(topics) == 0 {
		return
	}

	// A reminder repeated on every edit is nagging, and a reader learns to
	// skim past it. Each skill is named once per session; after that the model
	// has either loaded it or decided not to.
	var fresh []topic
	for _, t := range topics {
		if claim(p.SessionID, t.Skill) {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) == 0 {
		return
	}

	_ = json.NewEncoder(os.Stdout).Encode(output{hookSpecificOutput{
		HookEventName:     "PreToolUse",
		AdditionalContext: message(fresh, p),
	}})
}

func message(topics []topic, p payload) string {
	var b strings.Builder

	target := p.ToolInput.FilePath
	if target == "" {
		target = "this command"
	}

	for _, t := range topics {
		fmt.Fprintf(&b, "%s is %s. Invoke the `/%s` skill now, before writing or running anything, "+
			"and look the specifics up in its `%s` -- the full upstream reference is vendored in the "+
			"skill's `reference/` folder.\n",
			target, t.What, t.Skill, t.Read)
	}
	b.WriteString("Field names, flags and defaults recalled from memory are wrong often enough that " +
		"the reference exists; grep it rather than guessing. Said once per session.")

	return b.String()
}

// claim records that a skill has been named in this session, and reports
// whether this call is the one that named it.
//
// The marker is keyed by a hash of the session id so parallel sessions never
// silence each other, and it lives in the temp directory because it is worth
// nothing once the machine restarts. An unwritable temp directory means the
// reminder is sent every time rather than never: over-reminding is the lesser
// failure of the two.
func claim(sessionID, skill string) bool {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + skill))
	dir := filepath.Join(os.TempDir(), "docs-nudge")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return true
	}

	marker := filepath.Join(dir, hex.EncodeToString(sum[:8]))
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		// The marker already exists: this skill was named earlier.
		return !os.IsExist(err)
	}
	_ = f.Close()
	return true
}
