## No Busy Poll Plugin

The no-busy-poll plugin lives at `plugins/no-busy-poll/`. It is one Stop hook, modeled closely on the sibling `link-all-refs` and `no-blame-language` plugins.

Nothing about the answer can have changed in the seconds between calls. `babysit-workers.md` and `send-later-unavailable.md` already say what to do instead: wait for a queued notification, or arm a real wakeup (`ScheduleWakeup`, `send_later`, a `Monitor` watch) with a genuine delay. This plugin is the mechanical backstop for when that instruction gets skipped anyway.

That pattern is indistinguishable from a busy-poll loop by repetition count alone. The only thing that tells them apart is the gap between turns. So a streak only counts consecutive turns whose signatures match AND whose gap (this turn's start minus the previous turn's end) is under `maxGap` (default 5 minutes, `NO_BUSY_POLL_MAX_GAP_SECONDS`). A turn that repeats the call after a real gap breaks the streak rather than extending it -- `TestStreakBreaksOnALongGap` and `TestStopIsAllowedWhenTurnsAreProperlyPaced` pin this.

Each turn's signature is `ToolName|<canonical JSON input>` for every `tool_use` block it made, sorted and joined. Canonicalizing the input (unmarshal, then remarshal) means key order in the model's own JSON can never cause a false mismatch (`TestCanonicalJSONIgnoresKeyOrder`).

**The streak is measured against the CURRENT turn only.** `streak` walks backward from the last turn in the transcript. If that last turn's signature is empty, or differs from the one before it, or the gap to it exceeds `maxGap`. The count is below threshold and the stop is allowed.

Threshold defaults to 4 identical, closely-spaced turns (`NO_BUSY_POLL_THRESHOLD`, floored at 2 -- a threshold of 1 will refuse a turn that never repeated anything). The refusal message names the repeated call verbatim (Bash calls render their command. It escalates on `stop_hook_active` ("this is not the first refusal"), which is safe against ever wedging a session.

Every failure path allows the stop: an unparseable payload, a missing or unreadable transcript, and any `hook_event_name` other than `Stop` -- a guard that blocks because it can not read a file is worse than no guard.

- **Hook binary**: `plugins/no-busy-poll/hook.go` -- event dispatch, the Stop payload, the allow/refuse decision, and the refusal text
- **Detection**: `plugins/no-busy-poll/detect.go` -- the spacing-aware streak walk, and the `NO_BUSY_POLL_THRESHOLD`/`NO_BUSY_POLL_MAX_GAP_SECONDS` env overrides
- **Transcript**: `plugins/no-busy-poll/transcript.go` -- turn segmentation, the new-prompt boundary, call-signature canonicalization, and the bounded tail read
- **Tests**: `plugins/no-busy-poll/transcript_test.go`, `detect_test.go`, `hook_test.go` -- turn segmentation across a tool-result continuation, signature equality/inequality, the paced-watch-loop false-positive check, the current-turn-breaks-the-pattern case, and every fail-open path
- **Plugin config**: `plugins/no-busy-poll/.claude-plugin/plugin.json` -- the Stop registration and the PreToolUse registration on matcher `*`

## The deny half: a status read that cannot learn anything never runs

A session that asks after one pull request through `gh wait-ci`, then `gh wait-ci checks`, then `pull_request_read`. So the PreToolUse half refuses the call itself, keyed on the SUBJECT rather than the command text.

**A subject is the thing whose state is being asked after** -- a pull request or a commit (`subject.go`). It is read out of the tool name plus its input. The same extractor runs over the transcript's results. A subject that cannot be spelled consistently simply never matches, which allows rather than denies.

Two shapes are refused, and both answer one question -- can this call learn anything?

- **A settled subject** (`terminal.go`): a pull request this session watched merge or close, or a commit whose checks it watched go green. Neither can answer differently later. A push makes a NEW commit, so the green rule limits itself. A verdict in a record naming more than one pull request settles none of them. Nothing says which one it belongs to, and guessing there will refuse a read of one that is still open.
- Any of those re-opens every subject, so the refusal clears itself the moment something real happens rather than needing an override.

**Matching runs over an unescaped copy of each record. That is load-bearing.** A result's payload is a JSON string nested inside the record's own JSON. `unescape` also folds the `\uXXXX` spellings of `<`, `>` and `&`: a Go encoder escapes them and a JavaScript one does not. A guard that reads only one spelling fails open on the other without saying so.

Every failure path allows the call -- an unparseable payload, a missing transcript, a call naming no subject. A tool that is not a status read, and any other event. The matcher is `*` and the tool name is filtered in Go. The registration does not depend on matcher-regex semantics.

- **Records**: `plugins/no-busy-poll/records.go` -- the raw record walk, the wake-envelope markers, and the unescaping
- **Subjects**: `plugins/no-busy-poll/subject.go` -- the status-read tool and command tables, statement-position matching, and subject normalization
- **Verdicts**: `plugins/no-busy-poll/terminal.go` -- the merge and green markers, and the single-subject attribution rule
- **Decision**: `plugins/no-busy-poll/pretool.go` -- the signal window, the two refusals, and the deny payload
- **Tests**: `plugins/no-busy-poll/pretool_test.go` -- each refusal with its negative control (another pull request, another commit), a wake, a prompt and a push each re-opening a subject, a re-spelling through another tool still counting, a command that only mentions a status read not counting, both escaped spellings of a wake envelope, and every fail-open path
