## Focus Please Plugin

The focus-please plugin lives at `plugins/focus-please/`. It enforces "answer the human first": when a user prompt contains a `?`, the assistant may not ACT until it replies in plain text.

Stop also carries the **resume rule**: a prompt that arrives while a turn is still in flight is a mid-turn interjection. UserPromptSubmit detects this by finding the `active` marker still set (it is written on every prompt and removed only by an allowed Stop, so its presence means no Stop has fired since the last prompt) and records a `resume` marker. That marker makes the next Stop return exit code **2** with `resumeReason` on stderr, pushing the assistant back to the interrupted work. The refusal keys off the interruption, not the `?`, and is strictly one-shot. The flag is consumed before refusing, and `stop_hook_active` short-circuits to allow. A session can never be wedged shut. Because Stop clears `pending` before refusing, the continuation after a refusal starts with full tool access.

The three markers are `<tempdir>/focus-please/<sha256(session_id)[:16]>.{pending,active,resume}`, keyed per session so parallel sessions never interfere. Every failure path fails OPEN (no marker written, no denial emitted, no stop refused). All three `plugin.json` hook entries point at the same `build/focus-please` binary.

- **Hook binary**: `plugins/focus-please/hook.go` -- event dispatch, the three-marker lifecycle, the lookup allowlist, and the UserPromptSubmit/PreToolUse/Stop output shapes (`result` carries stdout + stderr + exit code)
- **Tests**: `plugins/focus-please/hook_test.go` . Covers arm/disarm, lookup-allow vs acting-deny, the full-turn cycle. Also the interjection->refuse-once->allow sequence, the `stop_hook_active` guard, per-session isolation of both the block and the resume flag, and fail-open on bad input
- **Plugin config**: `plugins/focus-please/.claude-plugin/plugin.json` -- the three hook registrations (UserPromptSubmit, PreToolUse matcher `*`, Stop)
