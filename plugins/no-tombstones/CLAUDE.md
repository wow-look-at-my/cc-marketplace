## No Tombstones Plugin

The no-tombstones plugin lives at `plugins/no-tombstones/`. The refusal is `permissionDecision: "deny"` with each finding, the tell that caught it. The line it sits on.

Or its AUDIENCE is the reviewer: it defends the change instead of telling the next editor what breaks. The one-line test is to delete the sentence and ask what the next editor now gets wrong -- no answer means it was narration.

**Three tiers, and the wording-based one is the weakest of them.** That ordering is deliberate, because the text being judged is written by a paraphrasing machine, and a rule keyed to a phrase is a rule it eventually writes around.

- It is a plain `[]tell`, so extending the plugin is adding a row. Order inside the table decides which tell a sentence is REPORTED as when several match, so the specific rules sit above the general ones.
- **The volume cap** (`FindTombstones`, default 14 lines, `NO_TOMBSTONES_MAX_COMMENT_LINES`) is the tier no rewording defeats. A tombstone is surplus text, so an essay whose every individual sentence reads as true and current still fails here. It governs SOURCE only -- a long paragraph in a document is ordinary writing, and applying it there will make every page unwritable.
- **The dead referent** (`referents.go`) catches the tombstone with no tell at all: "see `TestDarwinStatfsToLinux` for the pin", written beside the change that deleted that test. Precision lives entirely in `isCandidate`: an all-caps name is never eligible. A name under 8 characters is not eligible either, being too likely to be a word.

**The refusal MOVES the text. `relocate.go` appends the refused lines to `.git/TOMBSTONES` first and the refusal says where they went. The objection is pre-answered and the history lands where narrating a change belongs: the commit message. A failure to write the ledger never changes the verdict.

A document is split into paragraphs with fenced code, indented code, HTML comments and frontmatter skipped, and inline backtick spans blanked.

That run is also what caught the one false positive the unit tests had not: `that split has an obvious way to go wrong` is a NOUN, refused by the demonstrative-plus-participle rule.

**A known gap, stated rather than guessed at**: bare past-tense narration with no other tell ("section 6 listed the syscalls that fell through") is not matched. A general past-tense rule will fire on legitimate description of current behavior. The volume cap covers the same paragraph in source. In a document it does not.

Every failure path allows the call: an unparseable payload, an unparseable `tool_input`, an unjudged path. A tool that does not write files, and any `hook_event_name` other than `PreToolUse`. The matcher is `*` and the tool name is filtered in Go. The registration does not depend on matcher-regex semantics.

- **Hook binary**: `plugins/no-tombstones/hook.go` -- the PreToolUse payload, the three write shapes, the source-only volume cap, the refusal text and its truncation notice
- **Detection**: `plugins/no-tombstones/tells.go` -- the tell table, the shared-order rule, the volume cap, and per-line-per-tell dedupe
- **Extraction**: `plugins/no-tombstones/comments.go` -- the per-language comment scanner, block merging, and the document prose walker
- **Referents**: `plugins/no-tombstones/referents.go` -- identifier eligibility, the bounded ripgrep probe, and the working-tree walk
- **Relocation**: `plugins/no-tombstones/relocate.go` -- the append to `.git/TOMBSTONES`
- **Tests**: `plugins/no-tombstones/tells_test.go` (the release-note block split into the sentences that must be refused, each by a NAMED tell. The sentences that must survive), `hook_test.go` (every write shape with its unjudged-path control, the volume cap and its env override, relocation into a real repository, the dead-referent tier with a live-symbol control, and every fail-open path)
- **Plugin config**: `plugins/no-tombstones/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`

Keep the two in sync if either changes.
