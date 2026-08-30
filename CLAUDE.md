## No Busy Poll Plugin

The no-busy-poll plugin lives at `plugins/no-busy-poll/`. It is one Stop hook, modeled closely on the sibling `link-all-refs` and
`no-blame-language` plugins: a turn does not end while it is the latest in a run of several turns that each made the exact same tool call,
closely spaced in time, with nothing else different in between. The refusal is exit code 2 with the repeated call and the two ways out on
stderr, which is how a Stop hook hands the model its objection.

**The incident this exists for**: a session waiting on a PR merge ran `gh pr view 186 --json state,mergedAt` in an automated Stop-hook-driven
loop, dozens of times back to back, each time reporting a near-identical "still open, holding" message. Nothing about the answer could have
changed in the seconds between calls -- every re-check burned tokens for zero new signal, because no real event or scheduled wakeup separated
one call from the next. `babysit-workers.md` and `send-later-unavailable.md` already say what to do instead: wait for a queued notification,
or arm a real wakeup (`ScheduleWakeup`, `send_later`, a `Monitor` watch) with a genuine delay. This plugin is the mechanical backstop for when
that instruction gets skipped anyway.

**Spacing is the load-bearing half of the detector, not just the repetition count.** A naive "same call N times in a row" detector would
false-positive on a legitimate, org-endorsed paced watch loop -- the same status check, re-run every 15-30 minutes because a real scheduled
trigger fired each time. That pattern is indistinguishable from a busy-poll loop by repetition count alone; the only thing that tells them
apart is the gap between turns. So a streak only counts consecutive turns whose signatures match AND whose gap (this turn's start minus the
previous turn's end) is under `maxGap` (default 5 minutes, `NO_BUSY_POLL_MAX_GAP_SECONDS`). A turn that repeats the call after a real gap
breaks the streak rather than extending it -- `TestStreakBreaksOnALongGap` and `TestStopIsAllowedWhenTurnsAreProperlyPaced` pin this.

**A "turn" is segmented from the transcript, not read off `message.id`.** `parseTurns` walks the JSONL tail and starts a new turn on every
`type:"user"` record whose `content` is not entirely `tool_result` blocks -- a `tool_result` continuation answers a call from earlier in the
same turn and must not split it (`isNewPrompt`). Each turn's signature is `ToolName|<canonical JSON input>` for every `tool_use` block it
made, sorted and joined; canonicalizing the input (unmarshal, then remarshal) means key order in the model's own JSON can never cause a false
mismatch (`TestCanonicalJSONIgnoresKeyOrder`). A turn that made no tool call at all has an empty signature and can never extend or start a
streak -- replying with plain text is always a valid way to satisfy this hook (`TestStreakIsZeroWhenTheLastTurnMadeNoCall`).

**The streak is measured against the CURRENT turn only.** `streak` walks backward from the last turn in the transcript; if that last turn's
signature is empty, or differs from the one before it, or the gap to it exceeds `maxGap`, the count is below threshold and the stop is
allowed. This is why a model that heeds an earlier refusal and stops calling anything is never refused again for the calls it made before
(`TestStopIsAllowedWhenTheCurrentTurnBreaksThePattern`).

Threshold defaults to 4 identical, closely-spaced turns (`NO_BUSY_POLL_THRESHOLD`, floored at 2 -- a threshold of 1 would refuse a turn that
never repeated anything). The refusal message names the repeated call verbatim (Bash calls render their command; everything else renders
compact JSON), states the count, and gives the two ways out: reply with no tool call and wait for a real signal, or arm a real wakeup with a
genuine delay and stop -- never re-check by hand in the meantime. It escalates on `stop_hook_active` ("this is not the first refusal"), which
is safe against ever wedging a session, because `stop_hook_active=true` reflects a genuinely new turn produced in response to the earlier
block, not an instant re-invocation of unchanged content.

Every failure path allows the stop: an unparseable payload, a missing or unreadable transcript, and any `hook_event_name` other than `Stop` --
a guard that blocks because it could not read a file is worse than no guard.

- **Hook binary**: `plugins/no-busy-poll/hook.go` -- the Stop payload, the allow/refuse decision, and the refusal text
- **Detection**: `plugins/no-busy-poll/detect.go` -- the spacing-aware streak walk, and the `NO_BUSY_POLL_THRESHOLD`/`NO_BUSY_POLL_MAX_GAP_SECONDS` env overrides
- **Transcript**: `plugins/no-busy-poll/transcript.go` -- turn segmentation, the new-prompt boundary, call-signature canonicalization, and the bounded tail read
- **Tests**: `plugins/no-busy-poll/transcript_test.go`, `detect_test.go`, `hook_test.go` -- turn segmentation across a tool-result continuation, signature equality/inequality, the paced-watch-loop false-positive check, the current-turn-breaks-the-pattern case, and every fail-open path
- **Plugin config**: `plugins/no-busy-poll/.claude-plugin/plugin.json` -- one Stop hook registration
