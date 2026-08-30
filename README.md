# finish-your-todos

Enforces the task list at both ends of a turn.

Claude already receives a system reminder about keeping a task list on most
turns. It reads them and carries on: one observed session took five separate
assignments and filed **zero** tasks, then lost track of most of them. A
reminder is text, and text is skippable. These are refusals.

| Hook | When | What it does |
|---|---|---|
| `UserPromptSubmit` | you assign work | Records that a task is owed for this session |
| `PreToolUse` (`*`) | every tool call | Picks up assignments `UserPromptSubmit` never saw, then **denies** everything except the task tools until the task is filed |
| `Stop` | end of turn | **Blocks** the stop while any task is `pending` or `in_progress` |

`UserPromptSubmit` does not see every user message, which is why the entry gate
reads the transcript as well. A message sent **while a turn is running** is
enqueued rather than submitted and fires no prompt hook at all — and on a web
surface every message goes through that queue whenever the session is busy, so
those were most of them. Slash commands arrive raw too (`/goal fix the thing`,
never the expansion), so an assignment can hide in the arguments. Both channels
are covered from `PreToolUse`; see
[docs/missed-assignment-channels.md](docs/missed-assignment-channels.md).

## The entry gate

When a prompt looks like an assignment, the next tool call is refused with a
message quoting the assignment and naming the way out. `TaskCreate` files the
work; `TaskUpdate` covers work that maps onto a task already on the list. Both
settle the debt, and the refused call then goes through unchanged.

`TaskList` and `TaskGet` stay callable while blocked, so the list can be checked
for duplicates first — but reading the list does **not** settle the debt, or a
`TaskList` would buy silence without filing anything. There is deliberately no
"declare that there is no task" escape: an escape hatch is the hole the plugin
exists to close.

**What counts as an assignment** is biased toward yes, because a false positive
costs one `TaskCreate` and a false negative costs the whole point. Pure
questions ("why is this failing?"), bare acknowledgements ("ok", "thanks"), and
slash commands pass through. A question carrying an instruction ("why is that
failing? fix it") arms the gate — the instruction is the part that gets
forgotten. So does an auxiliary opener without a question mark ("do the thing",
"can you add a test"), which is an instruction, not a question.

## The exit gate

When Claude tries to stop, the hook reads the transcript and blocks (exit 2)
while anything is unfinished, naming what is left.

It understands **both** task surfaces: `TodoWrite`, where each call carries the
whole list, and the `TaskCreate`/`TaskUpdate` tools, where state is
reconstructed across the transcript (a create's result supplies the id for the
subject its call carried; each later update rewrites that task's status). That
second half matters — environments with the task tools have no `TodoWrite` at
all, so a gate that scanned only for `TodoWrite` had quietly stopped guarding.

The only way past is a list with nothing pending or in progress: finish the
work, or mark it completed/deleted so the list reflects reality.

### Loop protection

The Stop hook honors `stop_hook_active`. Once a stop is already being retried
because of a previous block, the hook steps aside. A single firm nudge turns an
*accidental* stop into a deliberate one, and a stuck session can never hang.

## Installation

```bash
/plugin install finish-your-todos
```

## Notes

- Everything fails open. Unparseable stdin, a missing session id, an unreadable
  transcript, or an unrecognized status allows the action rather than blocking
  it: a broken guard must never wedge a session.
- Per-session state lives in a temp file keyed by session id, so parallel
  sessions never collide.
- A session that never gets an assignment and never files a task is never
  touched by either hook.
