## No Blame Language Plugin

The no-blame-language plugin lives at `plugins/no-blame-language/`. It is one Stop hook, modeled closely on the sibling `link-all-refs` plugin. A turn does not end while its closing message uses deflecting, blame-shifting language.

**The banned-phrase table is data, not code.** `phrases.go`'s `bannedPhrases` is a plain `[]string`, so extending the list is editing one slice. `that predates this session`, `this was existing code`, `i only copied it`, and `git blame shows` are the provenance openers named verbatim in `you-wrote-it-own-it.md`. `pre-existing` and `preexisting` are the org owner's own explicit addition -- called out by name as a gap that must already have been closed. The rest (`not related to my change`, `unrelated to my diff`, and the `not my *` family) are direct synonyms of an entry already on the list, kept narrow rather than speculative.

**Matching is case-insensitive over whitespace-normalized text.** Every run of ASCII whitespace -- spaces, tabs, and newlines alike -- collapses to a single space before a phrase is searched for, so a phrase a markdown line-wrap split across two lines (`worth your\nattention`) still matches. `normalizeWhitespace` records, for every byte it keeps, the byte offset in the pre-normalized text it came from, so a match found in the collapsed string still traces back to its real line for the refusal to quote.

**The exemption logic is ported from `link-all-refs/refs.go`'s `assertedText`, not reinvented.** Fenced code, indented code, and blockquote lines are exempt. This very policy can be documented or discussed without tripping the hook.

**A genuine deferral is not banned.** Naming a real blocker plainly -- "this needs your call on A vs B, so I pushed the branch with A and left the test red" -- carries none of the banned phrases and is allowed.

Every failure path allows the stop: an unparseable payload, a missing or unreadable transcript, a message with no text, and a `hook_event_name` that is not `Stop` -- a guard that blocks because it can not read a file is worse than no guard.

- **Hook binary**: `plugins/no-blame-language/hook.go` -- the Stop payload, the allow/refuse decision, and the refusal text (which escalates on `stop_hook_active`, but still refuses. The model can always satisfy this by rewriting the message)
- **Detection**: `plugins/no-blame-language/phrases.go` -- the banned-phrase table, the compiled case-insensitive matchers, whitespace normalization with offset tracking, and the fenced/indented/blockquote exemption
- **Transcript**: `plugins/no-blame-language/transcript.go` -- the last assistant message's text blocks, read from a bounded tail (shared verbatim in design with `link-all-refs/transcript.go`)
- **Tests**: `plugins/no-blame-language/phrases_test.go`, `hook_test.go`, `transcript_test.go` -- every banned phrase actually blocks, case-insensitivity, whitespace normalization across a line wrap, the fenced/indented/blockquote exemption, inline backticks NOT being exempt, a fixed-and-owned finding and an honest deferral both being allowed, and every fail-open path
- **Plugin config**: `plugins/no-blame-language/.claude-plugin/plugin.json` -- one Stop hook registration
