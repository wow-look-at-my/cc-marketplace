## No Blame Language Plugin

The no-blame-language plugin lives at `plugins/no-blame-language/`. It is one **MessageDisplay** hook, modeled closely on the sibling `link-all-refs` plugin. A closing message that uses deflecting, blame-shifting language is MARKED. One line is appended to what the reader sees. It sends NOTHING back to the model, refuses nothing, and always exits 0.

**It was a Stop hook. That was wrong the same way link-all-refs' Stop hook was wrong.** A Stop hook runs after the message has streamed. Refusing therefore cannot unsend anything. The user reads the deflection, then reads a near-identical retype of the same message. The retype explains itself and names the banned phrase again. The guard fires a second time and asks for a third message with the same property. That loop has no bound. One goal was refused nine times over on it. The reader is the surface this rule is about. So the annotation goes there and the model is left alone. This is the standing rule for a guard here -- mitigate rather than refuse.

**`displayContent` is display-only**, read out of the shipped bundle rather than assumed. The event's own schema calls it "Text displayed in place of the delta". It states that the hook "replaces the delta on screen without changing the stored message". It is the only field the output schema carries. So this changes what the reader sees and nothing else.

**The message is judged whole, on its last flush.** A banned phrase can span a line wrap. One flush carries only the lines that completed since the last one. Judging a flush on its own therefore misses every phrase that straddles a boundary, and marks the same message several times over. `main.go` accumulates the deltas in a per-message state file under the temp directory, keyed by `message_id`. That is exactly how `link-all-refs` carries its fence state. The file is dropped on the final flush, and a sweep collects what a session that died mid-message left behind. Losing that file costs the phrases in the earlier flushes, never a wrong annotation.

**The annotation is one line.** It names at most three phrases and then the repair. It sits under the message the reader just read. It therefore has to stay short. The blockquote and the plugin name mark it as the hook talking rather than the model. The non-streaming path calls the hook once. That call carries `index` 0, `final` true, and the whole message as the delta. It is the same case with no accumulation in front of it.

**The banned-phrase table is data, not code.** `phrases.go`'s `bannedPhrases` is a plain `[]string`, so extending the list is editing one slice. `that predates this session`, `this was existing code`, `i only copied it`, and `git blame shows` are the provenance openers named verbatim in `you-wrote-it-own-it.md`. `pre-existing` and `preexisting` are the org owner's own explicit addition -- called out by name as a gap that must already have been closed. The rest (`not related to my change`, `unrelated to my diff`, and the `not my *` family) are direct synonyms of an entry already on the list, kept narrow rather than speculative.

**Matching is case-insensitive over whitespace-normalized text.** Every run of ASCII whitespace collapses to a single space before a phrase is searched for. A phrase a markdown line-wrap split across two lines (`worth your\nattention`) still matches. `normalizeWhitespace` records, for every byte it keeps, the byte offset it came from. A match found in the collapsed string still traces back to its real line.

**The exemption logic is ported from `link-all-refs/refs.go`'s `assertedText`, not reinvented.** Fenced code, indented code, and blockquote lines are exempt. This very policy can be documented or discussed without tripping the hook.

**A genuine deferral is not banned.** Naming a real blocker plainly -- "this needs your call on A vs B, so I pushed the branch with A and left the test red" -- carries none of the banned phrases and is never marked.

Every failure path prints nothing, which leaves the CLI showing the original text. Those are an unparseable payload, an empty payload, a message with no banned phrase, and a `hook_event_name` that is not `MessageDisplay`. This runs in the render path. A guard that can eat output is worse than no guard. `CC_NO_BLAME_LANGUAGE=0` disables it.

Nothing reads the transcript any more. The message arrives on the hook payload itself, flush by flush, so `transcript.go` is gone.

- **Entry point**: `plugins/no-blame-language/main.go` -- the MessageDisplay payload (`message_id`, `index`, `final`, `delta`), the `displayContent` envelope, and the per-message state file that accumulates the message across flushes
- **Annotation**: `plugins/no-blame-language/display.go` -- `Annotate` turns the findings into the one appended line, capped at three phrases
- **Detection**: `plugins/no-blame-language/phrases.go` -- the banned-phrase table, the compiled case-insensitive matchers, whitespace normalization with offset tracking, and the fenced/indented/blockquote exemption. Unchanged by the move off Stop
- **Tests**: `plugins/no-blame-language/phrases_test.go` covers detection. Every banned phrase is found. Also case-insensitivity, whitespace normalization across a line wrap, and the fenced/indented/blockquote exemption. Then inline backticks NOT being exempt, and a fixed-and-owned finding and an honest deferral both clean
- **Tests**: `plugins/no-blame-language/display_test.go` covers the event. The message itself is displayed unchanged with the line appended. Then a phrase split across two flushes, an empty final flush, the whole message in one flush, the one-annotation-per-message rule, and every fail-silent path. It also pins the RAW JSON keys: one `hookSpecificOutput` carrying `hookEventName` and `displayContent`, and no decision, reason or `systemMessage`
- **Plugin config**: `plugins/no-blame-language/.claude-plugin/plugin.json` -- one MessageDisplay hook registration
