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

**The rule is slopfmt's, and so is the plumbing.** `slopfmt hook --only tombstones` reads the PreToolUse payload on stdin and writes the response on stdout. It strips what it can, denies what it cannot, and holds no tell table, comment scanner or referent probe here. CI, an editor and this hook all shell out to the same binary, so none of them can drift from the others.

That plumbing used to live here, in Go, and identically again in `no-counts-in-docs`. The same payload parse, the same Write/Edit/MultiEdit shapes, the same splice back into `tool_input`. A write shape added to one and not the other is a guard that silently stops seeing half the writes.

**`hook.sh` exists for one reason: to fail OPEN.** A PreToolUse hook blocks the tool on any non-zero exit. Naming `slopfmt` straight in `plugin.json` therefore turns a missing binary into a guard that refuses every write in the session rather than none. The launcher probes for the binary and exits 0 when it is absent. `SLOPFMT` names another path, which is how a test drives a stub. `NO_TOMBSTONES_MAX_COMMENT_LINES` still sets the volume cap, passed through as `--max-comment-lines`.

- **Launcher**: `plugins/no-tombstones/hook.sh` -- the probe, the cap and the exec. This is the whole plugin
- **Rule and plumbing**: `wow-look-at-my/slopfmt`. Package `tombstones` holds the tell table, the comment scanner, the referent probe and the strip. `cmd/hook.go` holds the payload, the write shapes, the strip-or-deny decision and the response, and both are tested there
- **Plugin config**: `plugins/no-tombstones/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`

Keep the two in sync if either changes.
