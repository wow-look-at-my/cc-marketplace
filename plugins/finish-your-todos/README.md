# finish-your-todos

A Claude Code **Stop hook** that refuses to let the assistant end its turn while
its TodoWrite list still has unfinished items. It's a guard against the common
failure mode where Claude wraps up and stops even though the todo list it created
clearly still has work left in it.

## What it does

When Claude tries to stop, the hook:

1. Reads the conversation transcript and finds the most recent `TodoWrite` call
   (each call carries the complete list, so the latest one is the current state).
2. Counts items whose status is `pending` or `in_progress`.
3. If any remain, it **blocks the stop** (exit code 2) and feeds Claude a message
   naming the unfinished tasks, so Claude keeps working instead of stopping.
4. If every item is `completed` (or no todo list was ever created), it allows the
   stop.

### How to legitimately stop

The only way past the guard is a todo list with no pending or in-progress items.
That means Claude must either actually finish the work, or -- if a task is genuinely
done or no longer applicable -- update the list with `TodoWrite` (mark it
`completed` or remove it). The guard forces the todo list to reflect reality
rather than being abandoned mid-task.

### Loop protection

The hook honors the `stop_hook_active` flag from the Stop payload. Once a stop is
already being retried because of a previous block, the hook steps aside and allows
it. A single firm nudge turns an *accidental* stop into a deliberate one, and a
genuinely stuck session can never hang forever.

## Installation

```bash
/plugin install finish-your-todos
```

## Notes

- If no `TodoWrite` list exists in the session, the hook does nothing -- it never
  blocks a session that isn't using todos.
- It fails open: an unreadable transcript, malformed payload, or unrecognized
  status allows the stop rather than blocking it.
