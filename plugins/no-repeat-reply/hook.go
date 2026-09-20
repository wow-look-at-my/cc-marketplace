// Command no-repeat-reply is a Stop hook that breaks a single livelock: a
// gate re-fires, the assistant answers with the message it just sent, and
// the pair spin until the session dies without producing work.
//
// It refuses a single stop per session, and the refusal names the checks to
// run instead. The bound is a marker file rather than stop_hook_active,
// because the loop it catches lives entirely inside a stop-hook
// continuation, which is exactly when that flag is set. See CLAUDE.md.
//
// Every error path fails OPEN: no marker, no refusal, exit 0.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HookInput is the subset of the Stop payload this plugin reads.
type HookInput struct {
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}

// result is a single invocation's output: stderr carries the refusal
// reason, and exit 2 is how a Stop hook declines a stop.
type result struct {
	stderr string
	code   int
}

func allow() result { return result{} }

// minRepeatChars keeps a short acknowledgement ("Done.", "Pushed.") from
// reading as a stuck reply. A livelock message is a sentence, not a word.
const minRepeatChars = 24

const refusal = `You just sent the same closing message twice in a row.

A gate that re-fires hands you the turn back. It does not clear because you
repeat yourself: the next identical reply earns the identical rejection, and
a session has died in exactly that loop.

Do one of these instead, then end the turn:
  - Re-test the claim the message rests on. A route you called dead is the
    usual culprit, and the check is cheap (for a repo: list_repos, then
    add_repo with access "read", then git ls-remote).
  - Read what the gate asserts and check it against live state. A gate can be
    wrong. Proving that takes a command, not another sentence.
  - Do the work that IS available, and say what changed.
  - If the blocker is real and unchanged, say so once in NEW words, name the
    exact step that unblocks it, and stop restating it.

This hook refuses one stop per session. Your next stop goes through.`

func main() {
	res := run(os.Stdin)
	if res.stderr != "" {
		fmt.Fprint(os.Stderr, res.stderr)
	}
	os.Exit(res.code)
}

func run(stdin io.Reader) result {
	raw, err := io.ReadAll(stdin)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return allow()
	}
	var in HookInput
	if json.Unmarshal(raw, &in) != nil {
		return allow()
	}
	if in.HookEventName != "" && in.HookEventName != "Stop" {
		return allow()
	}
	if in.SessionID == "" || in.TranscriptPath == "" {
		return allow()
	}

	marker, err := markerPath(in.SessionID)
	if err != nil {
		return allow()
	}
	if _, err := os.Stat(marker); err == nil {
		return allow()
	}

	texts := closingTexts(in.TranscriptPath)
	if len(texts) < 2 {
		return allow()
	}
	last := normalize(texts[len(texts)-1])
	prev := normalize(texts[len(texts)-2])
	if len([]rune(last)) < minRepeatChars || last != prev {
		return allow()
	}

	// A marker that cannot be written would let this refuse every stop, so
	// the write failing means allowing this.
	if err := writeMarker(marker); err != nil {
		return allow()
	}
	return result{stderr: refusal + "\n", code: 2}
}

func markerPath(sessionID string) (string, error) {
	sum := sha256.Sum256([]byte(sessionID))
	name := hex.EncodeToString(sum[:])[:16]
	dir := filepath.Join(os.TempDir(), "no-repeat-reply")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func writeMarker(path string) error {
	return os.WriteFile(path, nil, 0o644)
}

// normalize drops what is not the signal. replies that differ only in
// whitespace or case are the same reply.
func normalize(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}
