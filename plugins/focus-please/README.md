# focus-please

When your message contains a question mark, this plugin blocks Claude from
acting until it answers you in plain text. Read-only lookups still work, so it
can check a file first if the answer needs it. It is a blunt "answer the human
first" guard for when the assistant keeps running tools instead of replying.

## How it works

One Go binary serves three hooks, keyed by per-session marker files:

- **UserPromptSubmit** — if your prompt contains `?`, it arms the block and adds
  a note telling Claude what is blocked. A prompt with no `?` disarms it.
- **PreToolUse** — while armed, tool calls are denied with a reason to answer
  you first. `Read`, `Grep` and `Glob` are exempt (including the MCP versions
  this marketplace ships), so Claude can keep looking around for an answer.
- **Stop** — when Claude finishes its reply the block lifts. If your message
  had interrupted a turn that was still running, this stop is refused **once**
  so Claude resumes the interrupted work instead of treating "I answered" as
  "I finished".

So a question costs Claude its ability to act, not its ability to look — and
interrupting mid-task no longer silently drops whatever it was doing.

## Install

```bash
/plugin marketplace add https://sites.pazer.build/cc-marketplace/branch/master/marketplace.json
/plugin install focus-please
```

## Notes

- Markers live at `<tempdir>/focus-please/<hash(session)>.<kind>`, keyed by
  session id, so parallel sessions never block one another.
- The stop refusal fires at most once per interruption and always yields to
  `stop_hook_active`, so a session can never be wedged shut.
- Every failure path fails open (no block, no refusal).
