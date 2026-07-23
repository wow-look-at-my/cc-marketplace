# focus-please

When your message contains a question mark, this plugin blocks **every** tool
call until Claude answers you in plain text. It is a blunt "answer the human
first" guard for when the assistant keeps running tools instead of replying.

## How it works

One Go binary serves three hooks, keyed by a per-session marker file:

- **UserPromptSubmit** — if your prompt contains `?`, it arms the block and adds
  a note telling Claude that tools are blocked until it replies. A prompt with
  no `?` disarms it.
- **PreToolUse** — while armed, any tool call is denied with a reason to answer
  you first.
- **Stop** — when Claude finishes its reply and ends the turn, the block clears,
  so the next turn's tools work normally.

The block therefore lasts exactly one turn: ask a question, Claude must reply
before it can touch a tool again.

## Install

```bash
/plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json
/plugin install focus-please
```

## Notes

- The marker lives at `<tempdir>/focus-please/<session>.pending`, keyed by
  session id, so parallel sessions never block one another.
- Every failure path fails open (no block), so the plugin can never wedge a
  session shut.
- It is deliberately aggressive: Claude cannot use tools *at all* on a turn where
  you asked a question, even to research the answer. That is the point — reply
  first, act next turn.
