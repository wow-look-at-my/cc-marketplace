# The channels an assignment can arrive on, and which ones fire a hook

The entry gate arms on `UserPromptSubmit`. That event does **not** see every
user message, and the gaps are not edge cases — they were the majority of one
session's assignments.

## What fires `UserPromptSubmit`

| How the message arrives | Fires? | What `prompt` contains |
|---|---|---|
| Typed prompt, session idle | **yes** | the raw text |
| Custom `.claude/commands/` command | **yes** | raw `/name args`, never the expansion |
| Local command returning a query (e.g. `/goal ...`) | **yes** | raw `/name args` |
| Local command returning text (e.g. `/effort high`) | **no** | — |
| **Message sent while a turn is running** | **no** | — |
| Scheduled / trigger-delivered prompt | only if drained at idle | — |
| Subagent prompt | **no** (`SubagentStart` instead) | — |

Two of those rows caused real losses.

## Gap 1: mid-turn messages fire nothing at all

A message sent while a turn is running is not submitted, it is **enqueued**. The
queue is drained inside the running turn and each entry becomes a
`queued_command` attachment. No `UserPromptSubmit` is dispatched anywhere on
that path.

This is not rare. On a bridge/web surface *every* inbound user message goes
through the queue, so any message that arrives while the session is busy — which
is nearly all of them, for a session doing work — was invisible to the gate.

In the session that prompted this fix, five such messages arrived. Every one
carried an instruction. None reached the gate:

```
also auto-allow the add repo and add repo root tool
no amends, no reverts, push your changes...
so you found a bug in the budget hook and didn't fix it? You're supposed to...
you can inspect the end result of the (previous) config in ~/.claude/settings.json
it's perfectly valid and expected that i give you new tasks mid-turn
```

The last one is the user stating the design intent outright.

**The fix:** `PreToolUse` fires on every tool call regardless of how the message
arrived, and every hook payload carries `transcript_path`. The gate re-reads the
transcript there and arms on anything not yet accounted for.

### The record to read

The authoritative shape is the attachment, not the rendered prose:

```json
{"type":"attachment","uuid":"...","attachment":{
  "type":"queued_command","commandMode":"prompt","prompt":"<the user's raw text>"}}
```

`prompt` is the raw text with no wrapper to strip, and `commandMode` separates a
typed message (`prompt`) from a harness-injected one (`task-notification`).

Verified against a live transcript. Note the rendered form lands inside a
`tool_result` block, not a `text` block — reading only `text` blocks finds
nothing, which is how the first attempt at this silently matched zero messages.

### System envelopes ride the same queue

Webhook events, background-task completions and reminders are queued exactly
like a typed message. Arming on those would refuse every tool call over a PR
notification nobody asked for, which is the fastest possible way to get the
whole plugin turned off. They are filtered by envelope prefix
(`<github-webhook-activity>`, `<task-notification>`, `<system-reminder>`, …)
*and* by `commandMode`.

### Arming at most once

A high-water mark (the last interjection's uuid) is stored beside the debt, in
its own file. It cannot live in the debt file: that is deleted every time a task
is filed, so a settled interjection would arm again on the very next tool call
and the session would never move.

## Gap 2: `/goal <work>` was skipped on the leading slash

The hook only ever sees the raw `/name args` — the expansion is invisible to it.
The classifier skipped anything starting with `/` as "the CLI's own control
surface", which is true of the command word and false of its arguments. Every
assignment handed over as `/goal fix the thing` was dropped.

Command arguments get a **stricter** rule than prose, because they are just as
often parameters. Prose that is not a question is almost always an instruction;
`/effort high` and `/loop 5m /babysit-prs` are not. So arguments arm when they
contain an imperative, or when they read as a sentence rather than a setting
(five words or more), and a question still needs an imperative to arm.

| input | arms | why |
|---|---|---|
| `/goal fix the flaky test` | yes | imperative |
| `/goal next up, figure out what is wrong with the plugin` | yes | reads as prose |
| `/goal do the thing` | no | short, no imperative — settings-shaped |
| `/effort high` | no | a setting |
| `/loop 5m /babysit-prs` | no | parameters |
| `/goal why is CI red?` | no | still a question |
| `/goal why is CI red? fix it` | yes | imperative riding a question |
| `/review` | no | no arguments |

## What is still not covered

- A local command returning text (`/effort high`) fires nothing, so no hook can
  see it. Not a loss: those are settings, not assignments.
- Subagent prompts fire `SubagentStart`, not this event. Out of scope — a
  subagent's work is the parent's task.
- A scheduled prompt folded mid-turn is caught by the transcript pass like any
  other queued entry; one drained at idle goes through `UserPromptSubmit`
  normally.
