## No Tombstones Plugin

The no-tombstones plugin lives at `plugins/no-tombstones/`. It mitigates rather than refuses. A tombstone that sits alone on one comment-only source line is deleted from the write. The write then goes through with `hookSpecificOutput.updatedInput`, plus one `additionalContext` line naming what was removed. Only a finding that cannot be excised as a clean whole line still gets `permissionDecision: "deny"`. Those are the volume cap, a document sentence, and a trailing comment sharing a code line. The refusal names each finding, the tell that caught it, and the line it sits on.

Or its AUDIENCE is the reviewer: it defends the change instead of telling the next editor what breaks. The one-line test is to delete the sentence and ask what the next editor now gets wrong -- no answer means it was narration.

**Three tiers, and the wording-based one is the weakest of them.** That ordering is deliberate. A paraphrasing machine writes the text being judged, and a rule keyed to a phrase is a rule it eventually writes around.

- It is a plain `[]tell`, so extending the plugin is adding a row. Order inside the table decides which tell a sentence is REPORTED as when several match, so the specific rules sit above the general ones.
- **The volume cap** (`FindTombstones`, default 14 lines, `NO_TOMBSTONES_MAX_COMMENT_LINES`) is the tier no rewording defeats. A tombstone is surplus text, so an essay whose every individual sentence reads as true and current still fails here. It governs SOURCE only -- a long paragraph in a document is ordinary writing, and applying it there will make every page unwritable.
- **The dead referent** catches the tombstone with no tell at all: "see `TestDarwinStatfsToLinux` for the pin", written beside the change that deleted that test. Precision lives entirely in `isCandidate`: an all-caps name is never eligible. A name under 8 characters is not eligible either, being too likely to be a word.

**A strippable finding is deleted outright, not relocated.** git history already holds anything worth keeping, so there is no ledger. The response's `additionalContext` names the removed line once. That is the only record. `Strippable` on a `Hit` means the line is BOTH the one raw source line the finding sits on AND that line's only content. No code and no sibling comment shares it. `Block` carries the parallel `lineNos`/`pure` arrays a comment scanner fills in for exactly this. A document's are left nil. A document hit is therefore never offered for stripping (see below). Deleting only ever removes whole lines from the write's own text (`content`, `new_string`, or one MultiEdit entry's `new_string`), never a mid-line span. A trailing `// tombstone` on a code line computes `pure=false`. So does a block comment's opening or closing line that shares code with `/*` or `*/`. Both fall through to deny instead of guessing at a splice.

A document is split into paragraphs with fenced code, indented code, HTML comments and frontmatter skipped, and inline backtick spans blanked. A document line is a paragraph under this org's no-hard-wrap convention, not a sentence. It commonly holds several sentences. Stripping the whole line risks deleting a keeper next to the tombstone. Every document hit is therefore forced `Strippable: false` and still denied.

That run is also what caught the one false positive the unit tests had not: `that split has an obvious way to go wrong` is a NOUN, refused by the demonstrative-plus-participle rule.

**A known gap, stated rather than guessed at**: bare past-tense narration with no other tell ("section 6 listed the syscalls that fell through") is not matched. A general past-tense rule will fire on legitimate description of current behavior. The volume cap covers the same paragraph in source. In a document it does not.

Every failure path allows the call: an unparseable payload, an unparseable `tool_input`, an unjudged path. A tool that does not write files, and any `hook_event_name` other than `PreToolUse`. The matcher is `*` and the tool name is filtered in Go. The registration does not depend on matcher-regex semantics.

**The rule is not here. It is slopfmt's.** This hook pipes each text a write adds through `slopfmt fix --only tombstones --json` and splices the answer back into the payload. It holds no tell table, no comment scanner and no referent probe of its own. CI, an editor and this hook all shell out to the same binary, so none of them can drift from the others. `SLOPFMT` names another path, which is how the tests drive a stub instead.

A missing binary, a timeout and an unreadable answer all let the write through unchanged. A guard that refuses a write because its tool is absent is worse than no guard.

What stays here is the plumbing. Which writes are worth a subprocess, how the answer is spliced back, the notice, and the refusal.

- **Hook binary**: `plugins/no-tombstones/hook.go` -- the PreToolUse payload, the per-unit call into slopfmt, the strip-or-deny decision, the `updatedInput` write-back, and the deny reason with its truncation notice. This is the whole plugin
- **Detection**: `wow-look-at-my/slopfmt`, package `tombstones`. The tell table, the comment scanner, the referent probe and the strip live there
- **Tests**: `plugins/no-tombstones/plumbing_test.go`. The suite drives a stub in place of the binary, because the rule is tested where it lives. Covered here is every write shape, a kept finding refusing the write, and every path that lets a write through
- **Plugin config**: `plugins/no-tombstones/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`

Keep the two in sync if either changes.
