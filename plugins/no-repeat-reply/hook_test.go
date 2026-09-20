package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closing builds a single assistant record that ENDED a turn with text.
func closing(text string) string {
	return record("assistant", "end_turn", []map[string]string{{"type": "text", "text": text}})
}

// midTurn builds an assistant record that stopped to call a tool.
func midTurn() string {
	return record("assistant", "tool_use", []map[string]string{{"type": "tool_use", "text": ""}})
}

func thinkingOnly() string {
	return record("assistant", "end_turn", []map[string]string{{"type": "thinking", "text": "pondering"}})
}

func record(kind, stop string, blocks []map[string]string) string {
	rec := map[string]any{
		"type": kind,
		"message": map[string]any{
			"stop_reason": stop,
			"content":     blocks,
		},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func transcript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return path
}

func payload(sessionID, transcriptPath string) string {
	b, err := json.Marshal(HookInput{
		HookEventName:  "Stop",
		SessionID:      sessionID,
		TranscriptPath: transcriptPath,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func otherEvent(event, sessionID, transcriptPath string) string {
	b, err := json.Marshal(HookInput{
		HookEventName:  event,
		SessionID:      sessionID,
		TranscriptPath: transcriptPath,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
}

const stuck = "Blocked. Waiting on you. Nothing further is available to me here."

func TestRefusesAnIdenticalClosingMessage(t *testing.T) {
	isolate(t)
	path := transcript(t, closing(stuck), midTurn(), closing(stuck))

	res := run(strings.NewReader(payload("s1", path)))

	assert.Equal(t, 2, res.code)
	assert.Contains(t, res.stderr, "same closing message twice")
	assert.Contains(t, res.stderr, "list_repos")
}

func TestWhitespaceAndCaseAreNotTheSignal(t *testing.T) {
	isolate(t)
	path := transcript(t, closing(stuck), closing("BLOCKED.   Waiting on you.\nNothing further is available to me here."))

	assert.Equal(t, 2, run(strings.NewReader(payload("s1", path))).code)
}

func TestRefusesOnlyOncePerSession(t *testing.T) {
	isolate(t)
	path := transcript(t, closing(stuck), closing(stuck))

	require.Equal(t, 2, run(strings.NewReader(payload("s1", path))).code)

	second := run(strings.NewReader(payload("s1", path)))
	assert.Equal(t, 0, second.code)
	assert.Empty(t, second.stderr)
}

func TestOneSessionsMarkerDoesNotSilenceAnother(t *testing.T) {
	isolate(t)
	path := transcript(t, closing(stuck), closing(stuck))

	require.Equal(t, 2, run(strings.NewReader(payload("s1", path))).code)
	assert.Equal(t, 2, run(strings.NewReader(payload("s2", path))).code)
}

func TestDifferentClosingMessagesAllowTheStop(t *testing.T) {
	isolate(t)
	path := transcript(t, closing(stuck), closing("Pushed the fix; CI is green on the new head."))

	assert.Equal(t, 0, run(strings.NewReader(payload("s1", path))).code)
}

// A short repeat is an acknowledgement, not a stuck reply.
func TestAShortRepeatIsAllowed(t *testing.T) {
	isolate(t)
	path := transcript(t, closing("Done."), closing("Done."))

	assert.Equal(t, 0, run(strings.NewReader(payload("s1", path))).code)
}

// Only the message the user READ counts. A repeated tool call or an
// identical thinking block is not a closing message.
func TestMidTurnAndThinkingRecordsAreNotClosingMessages(t *testing.T) {
	isolate(t)
	path := transcript(t, midTurn(), thinkingOnly(), midTurn(), thinkingOnly())

	assert.Equal(t, 0, run(strings.NewReader(payload("s1", path))).code)
}

func TestASingleClosingMessageAllowsTheStop(t *testing.T) {
	isolate(t)
	path := transcript(t, closing(stuck))

	assert.Equal(t, 0, run(strings.NewReader(payload("s1", path))).code)
}

func TestEveryBadInputFailsOpen(t *testing.T) {
	isolate(t)
	good := transcript(t, closing(stuck), closing(stuck))

	cases := map[string]string{
		"empty":            "",
		"not json":         "{not json",
		"no session id":    payload("", good),
		"no transcript":    payload("s1", ""),
		"missing file":     payload("s1", filepath.Join(t.TempDir(), "absent.jsonl")),
		"another event":    otherEvent("PreToolUse", "s1", good),
		"garbage jsonl":    payload("s1", transcript(t, "{{{", "not a record")),
		"empty transcript": payload("s1", transcript(t, "")),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			res := run(strings.NewReader(in))
			assert.Equal(t, 0, res.code)
			assert.Empty(t, res.stderr)
		})
	}
}
