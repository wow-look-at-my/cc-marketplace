package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func call(session, tool string, input toolInput) payload {
	return payload{
		HookEventName: "PreToolUse",
		SessionID:     session,
		ToolName:      tool,
		ToolInput:     input,
	}
}

func TestDecideNamesTheSkillAndTheReferenceFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	out, say := decide(call("a", "Write", toolInput{FilePath: "svc/Dockerfile"}))

	require.True(t, say)
	text := out.HookSpecificOutput.AdditionalContext
	assert.Contains(t, text, "/docs:dockerfile")
	assert.Contains(t, text, "reference/dockerfile.md")
	assert.Contains(t, text, "svc/Dockerfile", "the message names the actual target")
}

// A Bash call has no file path, so the message must still read sensibly.
func TestDecideDescribesACommandWithNoPath(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	out, say := decide(call("a", "Bash", toolInput{Command: "docker compose up -d"}))

	require.True(t, say)
	text := out.HookSpecificOutput.AdditionalContext
	assert.Contains(t, text, "this command")
	assert.Contains(t, text, "/docs:docker-compose")
	assert.NotContains(t, text, "  is", "no gap where a filename would have been")
}

func TestDecideMentionsBothSkillsWhenBothApply(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	out, say := decide(call("a", "Bash", toolInput{Command: "docker compose build"}))

	require.True(t, say)
	text := out.HookSpecificOutput.AdditionalContext
	assert.Contains(t, text, "/docs:dockerfile")
	assert.Contains(t, text, "/docs:docker-compose")
}

func TestDecideStaysQuietOnASecondCall(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	c := call("a", "Write", toolInput{FilePath: "Dockerfile"})

	_, first := decide(c)
	_, second := decide(c)

	assert.True(t, first)
	assert.False(t, second)
}

func TestDecideIgnoresAnotherEvent(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	c := call("a", "Write", toolInput{FilePath: "Dockerfile"})
	c.HookEventName = "PostToolUse"

	_, say := decide(c)

	assert.False(t, say)
}

func TestDecideIgnoresAnUnrelatedFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	_, say := decide(call("a", "Write", toolInput{FilePath: "server.go"}))

	assert.False(t, say)
}

func TestClaimIsPerSessionAndPerSkill(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	assert.True(t, claim("s1", "docs:dockerfile"), "first time")
	assert.False(t, claim("s1", "docs:dockerfile"), "already said")
	assert.True(t, claim("s1", "docs:docker-compose"), "a different skill")
	assert.True(t, claim("s2", "docs:dockerfile"), "a different session")
}

// Over-reminding is the lesser failure: an unwritable temp directory must not
// make the hook silent, because silence is indistinguishable from working.
func TestClaimSpeaksUpWhenItCannotRecord(t *testing.T) {
	t.Setenv("TMPDIR", "/proc/nonexistent-and-unwritable")

	assert.True(t, claim("s1", "docs:dockerfile"))
	assert.True(t, claim("s1", "docs:dockerfile"))
}

// With no session id there is nothing to tell sessions apart, so one shared
// marker would silence every session after the first.
func TestClaimSpeaksUpWithNoSessionID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	assert.True(t, claim("", "docs:dockerfile"))
	assert.True(t, claim("", "docs:dockerfile"))
}

func TestMessageEndsWithTheGrepInstruction(t *testing.T) {
	text := message([]topic{dockerfileTopic}, call("a", "Write", toolInput{FilePath: "Dockerfile"}))

	assert.True(t, strings.Contains(text, "grep it rather than guessing"))
	assert.Contains(t, text, "once per session", "says why it will not repeat")
}
