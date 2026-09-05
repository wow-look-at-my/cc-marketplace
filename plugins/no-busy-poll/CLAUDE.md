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

- **Hook binary**: `plugins/no-busy-poll/hook.go` -- event dispatch, the Stop payload, the allow/refuse decision, and the refusal text
- **Detection**: `plugins/no-busy-poll/detect.go` -- the spacing-aware streak walk, and the `NO_BUSY_POLL_THRESHOLD`/`NO_BUSY_POLL_MAX_GAP_SECONDS` env overrides
- **Transcript**: `plugins/no-busy-poll/transcript.go` -- turn segmentation, the new-prompt boundary, call-signature canonicalization, and the bounded tail read
- **Tests**: `plugins/no-busy-poll/transcript_test.go`, `detect_test.go`, `hook_test.go` -- turn segmentation across a tool-result continuation, signature equality/inequality, the paced-watch-loop false-positive check, the current-turn-breaks-the-pattern case, and every fail-open path
- **Plugin config**: `plugins/no-busy-poll/.claude-plugin/plugin.json` -- the Stop registration and the PreToolUse registration on matcher `*`

## The deny half: a status read that cannot learn anything never runs

The Stop half refuses to END a turn, which means the wasted calls are already
paid for by the time it speaks, and it compares call SIGNATURES, which a
re-spelling defeats. A session that asks after one pull request through
`gh wait-ci`, then `gh wait-ci checks`, then `pull_request_read`, then
`list_commits` produces a different signature every turn and no streak at all,
while every call learns the same nothing. So the PreToolUse half refuses the
call itself, keyed on the SUBJECT rather than the command text.

**A subject is the thing whose state is being asked after** -- a pull request
or a commit (`subject.go`). It is read out of the tool name plus its input, so
`gh pr view 87 --repo owner/name`, `{"owner":...,"repo":...,"pullNumber":87}`
and a bare `owner/name#87` all normalize to one key. The same extractor runs
over the transcript's results, so a subject that cannot be spelled
consistently simply never matches, which allows rather than denies.

Two shapes are refused, and both answer one question -- can this call learn
anything?

- **A settled subject** (`terminal.go`): a pull request this session watched
  merge or close, or a commit whose checks it watched go green. Neither can
  answer differently later, and a push makes a NEW commit, so the green rule
  limits itself. A verdict in a record naming more than one pull request
  settles none of them: nothing says which one it belongs to, and guessing
  there would refuse a read of one that is still open.
- **A subject already read with no signal since** (`pretool.go`): no user
  message, no wake or notification envelope, and no push or commit of the
  session's own. Any of those re-opens every subject, so the refusal clears
  itself the moment something real happens rather than needing an override.

**Matching runs over an unescaped copy of each record, and that is
load-bearing.** A result's payload is a JSON string nested inside the
record's own JSON, so a verdict reaches the transcript with its quotes
backslashed and the marker text never appears in the raw line. `unescape`
also folds the `\uXXXX` spellings of `<`, `>` and `&`: a Go encoder escapes
them and a JavaScript one does not, and a guard that reads only one spelling
fails open on the other without saying so.

Every failure path allows the call -- an unparseable payload, a missing
transcript, a call naming no subject, a tool that is not a status read, and
any other event. The matcher is `*` and the tool name is filtered in Go, so
the registration does not depend on matcher-regex semantics.

- **Records**: `plugins/no-busy-poll/records.go` -- the raw record walk, the wake-envelope markers, and the unescaping
- **Subjects**: `plugins/no-busy-poll/subject.go` -- the status-read tool and command tables, statement-position matching, and subject normalization
- **Verdicts**: `plugins/no-busy-poll/terminal.go` -- the merge and green markers, and the single-subject attribution rule
- **Decision**: `plugins/no-busy-poll/pretool.go` -- the signal window, the two refusals, and the deny payload
- **Tests**: `plugins/no-busy-poll/pretool_test.go` -- each refusal with its negative control (another pull request, another commit), a wake, a prompt and a push each re-opening a subject, a re-spelling through another tool still counting, a command that only mentions a status read not counting, both escaped spellings of a wake envelope, and every fail-open path
