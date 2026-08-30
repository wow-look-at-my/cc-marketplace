# no-busy-poll

A turn does not end while it is the latest in a run of turns that each made the exact same tool call, spaced too
close together for anything to actually have changed.

Re-running the same status check every turn instead of waiting for a real event burns tokens for zero new signal --
the answer cannot have changed in the seconds since the last check. The Stop hook refuses the stop and names the call:

```
Stop. The last 4 turns in a row made the exact same call, with nothing else
different in between and no real wait between them -- that is a busy-poll loop,
and it burns the user's tokens for zero new signal, because the answer cannot
have changed in the seconds since the last check:

  Bash: gh pr view 186 --json state,mergedAt

Do not run any of the calls above again on a hunch. Either:

  - Reply with NO tool call at all and wait for a real signal -- a queued
    notification, a scheduled trigger firing, an actual event arriving -- or
  - Arm a real wakeup (ScheduleWakeup / send_later / a Monitor watch) with a
    genuine delay, then stop. Never re-check by hand in the meantime.

Rewrite this turn so it makes none of the calls listed above, then stop.
```

## What it does not flag

A properly paced watch loop -- the same check, re-run every 15-30 minutes because a real scheduled trigger fired --
is not this pattern. Spacing, not repetition count, is what tells the two apart: a streak only extends across turns
whose gap stays under the max (default 5 minutes). A real gap between two identical checks breaks the streak instead
of counting toward it.

A turn that makes no tool call at all -- pure text, waiting -- always resets the streak. Replying with nothing to run
is always a valid way to satisfy this hook.

## Tuning

| Env var | Default | Meaning |
|---|---|---|
| `NO_BUSY_POLL_THRESHOLD` | `4` | Turns in a row before the stop is refused |
| `NO_BUSY_POLL_MAX_GAP_SECONDS` | `300` | Longest gap between turns that still counts as the same streak |

## Install

```sh
claude plugin marketplace add wow-look-at-my/cc-marketplace
claude plugin install no-busy-poll
```
