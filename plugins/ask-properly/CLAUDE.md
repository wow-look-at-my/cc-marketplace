## Ask Properly Plugin

The ask-properly plugin lives at `plugins/ask-properly/`. It is one Stop hook, modeled closely on the sibling `link-all-refs`, `no-blame-language` and `no-busy-poll` plugins. A turn does not end while its closing message hands the user a decision in prose. The refusal is exit code 2 with each finding. The line it sits on, and the two ways out on stderr, which is how a Stop hook hands the model its objection.

**The incident this exists for**: a session resolving a spec's open questions was answered on one card and asked a question in the same message. It recorded the answer, wrote the doc, and never answered the question. Told "you didn't answer my question", it replied that it will not re-ask on a card since the last one was dismissed. Prose does neither.

The check is per TURN, not per session -- an `AskUserQuestion` in an EARLIER turn does not license a prose question now (`TestAnEarlierTurnsAskDoesNotCount`). Turn segmentation is ported from `no-busy-poll`.

**A "?" alone cannot decide this. That is the whole detector.** This org's specs are full of nullable types (`Int?`, `raw_args?`) and every compare URL carries `?expand=1`. Two words, not more: "The field is Int? and ..." reaches `is` on the third word and is a statement. Both halves are needed. An earlier draft matched a cue anywhere in the sentence and reported every nullable type in a paragraph that mentioned one.

**The deferral table is data, not code.** `deferralPhrases` is a plain `[]string`, so extending it is editing one slice. A message may state what it did and stop. It may not close by inviting the user to decide in prose.

Or call `AskUserQuestion` with the recommendation first and labelled, every option describing what it costs and what it buys. It also states that a dismissed or unanswered card is not a ban on asking -- that misreading is half the incident above. On `stop_hook_active` it escalates, and adds that deleting the question while leaving the decision unmade does not satisfy it.

Fenced code, indented code and blockquotes are exempt (ported from `link-all-refs/refs.go`'s `assertedText`). This policy can be written down without tripping the hook. Inline backticks are NOT exempt, matching both siblings.

- **Hook binary**: `plugins/ask-properly/hook.go` -- the Stop payload, the used-the-tool allow, the refusal text and its escalation
- **Detection**: `plugins/ask-properly/questions.go` -- the deferral table, the cue list, link stripping with offset tracking, the end-of-line/opens-with-cue rule, and the fenced/indented/blockquote exemption
- **Transcript**: `plugins/ask-properly/transcript.go` -- the last assistant message's text plus whether this turn called `AskUserQuestion`, read from a bounded tail
- **Tests**: `plugins/ask-properly/questions_test.go`, `hook_test.go`, `transcript_test.go` -- nullable types and compare URLs NOT tripping it, a real question and every deferral phrase blocking, the tool-used allow, an earlier turn's call not counting, a tool result not splitting a turn, and every fail-open path
- **Plugin config**: `plugins/ask-properly/.claude-plugin/plugin.json` -- one Stop hook registration

Keep the two in sync if either changes.
