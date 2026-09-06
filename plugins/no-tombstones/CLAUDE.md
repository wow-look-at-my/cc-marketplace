## No Tombstones Plugin

The no-tombstones plugin lives at `plugins/no-tombstones/`. It mitigates rather than refuses: a tombstone that sits alone on one comment-only source line is deleted from the write, which is then allowed through with `hookSpecificOutput.updatedInput`, plus one `additionalContext` line naming what was removed. Only a finding that cannot be excised as a clean whole line -- the volume cap, a document sentence, a trailing comment sharing a code line -- still gets `permissionDecision: "deny"`, with each finding, the tell that caught it, and the line it sits on.

Or its AUDIENCE is the reviewer: it defends the change instead of telling the next editor what breaks. The one-line test is to delete the sentence and ask what the next editor now gets wrong -- no answer means it was narration.

**Three tiers, and the wording-based one is the weakest of them.** That ordering is deliberate. A paraphrasing machine writes the text being judged, and a rule keyed to a phrase is a rule it eventually writes around.

- It is a plain `[]tell`, so extending the plugin is adding a row. Order inside the table decides which tell a sentence is REPORTED as when several match, so the specific rules sit above the general ones.
- **The volume cap** (`FindTombstones`, default 14 lines, `NO_TOMBSTONES_MAX_COMMENT_LINES`) is the tier no rewording defeats. A tombstone is surplus text, so an essay whose every individual sentence reads as true and current still fails here. It governs SOURCE only -- a long paragraph in a document is ordinary writing, and applying it there will make every page unwritable.
- **The dead referent** (`referents.go`) catches the tombstone with no tell at all: "see `TestDarwinStatfsToLinux` for the pin", written beside the change that deleted that test. Precision lives entirely in `isCandidate`: an all-caps name is never eligible. A name under 8 characters is not eligible either, being too likely to be a word.

**A strippable finding is deleted outright, not relocated.** git history already holds anything worth keeping, so there is no ledger: the response's `additionalContext` names the removed line once, and that is the only record. `Strippable` on a `Hit` means the line is BOTH the one raw source line the finding sits on AND that line's only content -- no code, no sibling comment shares it. `Block` carries the parallel `lineNos`/`pure` arrays a comment scanner fills in for exactly this; a document's are left nil, so a document hit is never offered for stripping (see below). Deleting only ever removes whole lines from the write's own text (`content`, `new_string`, or one MultiEdit entry's `new_string`), never a mid-line span: a trailing `// tombstone` on a code line, or a block comment's opening/closing line sharing code with `/*`/`*/`, computes `pure=false` and falls through to deny instead of guessing at a splice.

A document is split into paragraphs with fenced code, indented code, HTML comments and frontmatter skipped, and inline backtick spans blanked. A document line is a paragraph under this org's no-hard-wrap convention, not a sentence, so it commonly holds several sentences; stripping the whole line risks deleting a keeper next to the tombstone. Every document hit is therefore forced `Strippable: false` and still denied.

That run is also what caught the one false positive the unit tests had not: `that split has an obvious way to go wrong` is a NOUN, refused by the demonstrative-plus-participle rule.

**A known gap, stated rather than guessed at**: bare past-tense narration with no other tell ("section 6 listed the syscalls that fell through") is not matched. A general past-tense rule will fire on legitimate description of current behavior. The volume cap covers the same paragraph in source. In a document it does not.

Every failure path allows the call: an unparseable payload, an unparseable `tool_input`, an unjudged path. A tool that does not write files, and any `hook_event_name` other than `PreToolUse`. The matcher is `*` and the tool name is filtered in Go. The registration does not depend on matcher-regex semantics.

- **Hook binary**: `plugins/no-tombstones/hook.go` -- the PreToolUse payload, the per-unit scan (one per Write/Edit/MultiEdit-entry text), the strip-or-deny decision, the `updatedInput` write-back, and the deny reason with its truncation notice
- **Detection**: `plugins/no-tombstones/tells.go` -- the tell table, the shared-order rule, the volume cap, per-line-per-tell dedupe, and `linePurity`/`hitForName` turning a match into a `Hit` with its strip metadata
- **Extraction**: `plugins/no-tombstones/comments.go` -- the per-language comment scanner (now tracking each line's source position and whether deleting it is clean), block merging, and the document prose walker
- **Referents**: `plugins/no-tombstones/referents.go` -- identifier eligibility, the bounded ripgrep probe, and the working-tree walk
- **Tests**: `plugins/no-tombstones/tells_test.go` (the release-note block split into the sentences that must be refused, each by a NAMED tell. The sentences that must survive), `hook_test.go` (every write shape stripping its own comment-only line, the trailing-comment and block-comment-edge negative controls that must still deny, MultiEdit touching only its own offending edit, the volume cap and document paths always denying, the dead-referent tier stripping rather than denying, and every fail-open path)
- **Plugin config**: `plugins/no-tombstones/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`

Keep the two in sync if either changes.
