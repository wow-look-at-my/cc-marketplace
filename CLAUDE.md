## Ask Properly Plugin

The ask-properly plugin lives at `plugins/ask-properly/`. It is one **MessageDisplay** hook, modeled closely on the sibling `link-all-refs` and `no-blame-language` plugins. A closing message that hands the user a decision in prose is MARKED. One line is appended to what the reader sees, naming the finding and the two ways out. It sends NOTHING back to the model, refuses nothing, and always exits 0.

**It was a Stop hook. That was wrong the same way link-all-refs' Stop hook was wrong.** A Stop hook runs after the message has streamed. Refusing therefore cannot unsend anything. The user reads the prose question, then reads a near-identical retype of the same message. The retype explains itself and puts the decision back into prose. The guard fires a second time. That loop has no bound. The reader is the person the question was aimed at. So the annotation goes there and the model is left alone. This is the standing rule for a guard here -- mitigate rather than refuse.

**`displayContent` is display-only**, read out of the shipped bundle rather than assumed. The event's own schema calls it "Text displayed in place of the delta". It states that the hook "replaces the delta on screen without changing the stored message". It is the only field the output schema carries.

**The message is judged whole, on its last flush.** A question can span a line wrap. One flush carries only the lines that completed since the last one. Judging a flush on its own therefore misses every sentence that straddles a boundary, and marks the same message several times over. `main.go` accumulates the deltas in a per-message state file under the temp directory, keyed by `message_id`. The file is dropped on the final flush, and a sweep collects what a session that died mid-message left behind. The non-streaming path calls the hook once instead. That call carries `index` 0, `final` true, and the whole message as the delta.

**The annotation is one line.** It names at most three findings and then the repair. A deferral phrase sitting inside a question already quoted is dropped, because "Want me to fix it?" and `want me to` are one finding said twice. A question hit is stored without the `?` that closed it. The mark therefore goes back on. A long sentence is trimmed from the front.

**The AskUserQuestion escape hatch survives, on the payload's own `transcript_path`.** Every hook event carries the base fields, `transcript_path` among them. A card put up EARLIER in this turn is therefore visible while the message renders. A card in the very message being displayed is not recorded yet. That message can still pick up the line. The cost of that is one advisory line under a message. It is never a refusal, which is what makes the weaker check acceptable here.

**The incident this exists for**: a session resolving a spec's open questions was answered on one card and asked a question in the same message. It recorded the answer, wrote the doc, and never answered the question. Told "you didn't answer my question", it replied that it will not re-ask on a card since the last one was dismissed. Prose does neither.

The check is per TURN, not per session -- an `AskUserQuestion` in an EARLIER turn does not license a prose question now (`TestAnEarlierTurnsAskDoesNotCount`). Turn segmentation is ported from `no-busy-poll`.

**A "?" alone cannot decide this. That is the whole detector.** This org's specs are full of nullable types (`Int?`, `raw_args?`) and every compare URL carries `?expand=1`. Two words, not more: "The field is Int? and ..." reaches `is` on the third word and is a statement. Both halves are needed. An earlier draft matched a cue anywhere in the sentence and reported every nullable type in a paragraph that mentioned one.

**The deferral table is data, not code.** `deferralPhrases` is a plain `[]string`, so extending it is editing one slice. A message may state what it did and stop. It may not close by inviting the user to decide in prose.

The appended line carries both ways out. Answer it yourself and say what you assumed. Or call `AskUserQuestion` with the recommendation first and labelled, every option describing what it costs and what it buys. A dismissed or unanswered card is not a ban on asking -- that misreading is half the incident above.

Fenced code, indented code and blockquotes are exempt (ported from `link-all-refs/refs.go`'s `assertedText`). This policy can be written down without tripping the hook. Inline backticks are NOT exempt, matching both siblings.

Every failure path prints nothing, which leaves the CLI showing the original text. Those are an unparseable payload, an empty payload, a message with no finding, and a `hook_event_name` that is not `MessageDisplay`. An unreadable transcript costs the escape hatch and nothing else. This runs in the render path. A guard that can eat output is worse than no guard. `CC_ASK_PROPERLY=0` disables it.

- **Entry point**: `plugins/ask-properly/main.go` -- the MessageDisplay payload (`transcript_path`, `message_id`, `index`, `final`, `delta`), the used-the-tool exit, the `displayContent` envelope, and the per-message state file that accumulates the message across flushes
- **Annotation**: `plugins/ask-properly/display.go` -- `Annotate` turns the findings into the one appended line, the deduplication against a quoted question, and the per-finding bound
- **Detection**: `plugins/ask-properly/questions.go` -- the deferral table, the cue list, link stripping with offset tracking, the end-of-line/opens-with-cue rule, and the fenced/indented/blockquote exemption. Unchanged by the move off Stop
- **Transcript**: `plugins/ask-properly/transcript.go` -- whether this turn called `AskUserQuestion`, read from a bounded tail. It no longer reads the message text, which now arrives on the payload
- **Tests**: `plugins/ask-properly/questions_test.go` covers detection, including nullable types and compare URLs NOT tripping it
- **Tests**: `plugins/ask-properly/display_test.go` covers the event. The message itself is displayed unchanged with the line appended. Then a card earlier in the turn silencing it, and an earlier turn's card not counting. Then a question split across two flushes, an empty final flush, and every fail-silent path. It also pins the RAW JSON keys: one `hookSpecificOutput` carrying `hookEventName` and `displayContent`, and no decision, reason or `systemMessage`
- **Tests**: `plugins/ask-properly/transcript_test.go` covers the turn boundary, including a tool result not splitting a turn
- **Plugin config**: `plugins/ask-properly/.claude-plugin/plugin.json` -- one MessageDisplay hook registration
