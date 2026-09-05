## No Tombstones Plugin

The no-tombstones plugin lives at `plugins/no-tombstones/`. It is one PreToolUse hook, modeled closely on the sibling `no-counts-in-docs`
plugin: a `Write`, `Edit` or `MultiEdit` is refused when the prose it ADDS is a tombstone -- a comment about a state the code is no longer in,
or an argument aimed at whoever reviews the diff rather than at whoever edits the file next. The refusal is `permissionDecision: "deny"` with
each finding, the tell that caught it, and the line it sits on.

**A tombstone fails one of two properties, and naming which is what the refusal is for.** Its REFERENT is gone: the flag, the test, the old
spelling it describes is not in the tree any more. Or its AUDIENCE is the reviewer: it defends the change instead of telling the next editor
what breaks. The one-line test is to delete the sentence and ask what the next editor now gets wrong -- no answer means it was narration.
Prose alone was already loaded in `claude_snippets/no-history-in-comments.md` and `comments-not-essays.md` and did not stop it, which is why
this is a deny at the moment of the write rather than a note in context.

**Three tiers, and the wording-based one is the weakest of them.** That ordering is deliberate, because the text being judged is written by a
paraphrasing machine, and a rule keyed to a phrase is a rule it eventually writes around.

- **The tell table** (`tells.go`) is the cheap first pass: dates, change references, former-state and then-and-now markers, addresses to the
  reviewer, defences of the change, quoted instructions, and reports of an experiment. It is a plain `[]tell`, so extending the plugin is
  adding a row. Order inside the table decides which tell a sentence is REPORTED as when several match, so the specific rules sit above the
  general ones.
- **The volume cap** (`FindTombstones`, default 14 lines, `NO_TOMBSTONES_MAX_COMMENT_LINES`) is the tier no rewording defeats. A tombstone is
  surplus text, so an essay whose every individual sentence reads as true and current still fails here. It governs SOURCE only -- a long
  paragraph in a document is ordinary writing, and applying it there would make every page unwritable.
- **The dead referent** (`referents.go`) catches the tombstone with no tell at all: "see `TestDarwinStatfsToLinux` for the pin", written
  beside the change that deleted that test. Precision lives entirely in `isCandidate`: an all-caps name is never eligible, because `ENOSYS`,
  `RLIMIT_CORE` and `SYS_STATFS` are the shape a low-level comment is full of and none of them is the repository's to define; a name under 8
  characters is not eligible either, being too likely to be a word. Absence of an answer -- no repository, no ripgrep, a timeout, a search
  that errors -- reports nothing, because "I could not look" must never read as "the symbol is gone".

**The refusal MOVES the text, it does not delete it.** A deny that says "remove that sentence" gets argued with, because the sentence is
usually TRUE and the model writing it knows so. `relocate.go` appends the refused lines to `.git/TOMBSTONES` first and the refusal says where
they went, so the objection is pre-answered and the history lands where narrating a change belongs: the commit message. A failure to write the
ledger never changes the verdict.

**Only the prose is judged, never the code.** `comments.go` extracts comment text with a string-literal-aware scanner, so
`msg := "this PR previously removed the flag"` is data rather than the file's own voice; a document is split into paragraphs with fenced code,
indented code, HTML comments and frontmatter skipped, and inline backtick spans blanked. An extension absent from `byExt`/`byBase` is not
judged at all -- guessing at a syntax risks reading code as prose, and a deny built on that is the kind a user turns off.

**Verified against real prose, not only unit tests.** The plugin was run over the release-note block it was designed from: it named the two
dates, the "emulated now" contrast, the "what is worth keeping here" aside, and both halves of the "that is not tidying ... would have been"
defence, while every keeper passed -- the nosplit ceiling, the struct sizes, the EINVAL guard, the `getpriority` errno rule, the `sendfile`
argument order. That run is also what caught the one false positive the unit tests had not: `that split has an obvious way to go wrong` is a
NOUN, refused by the demonstrative-plus-participle rule. The ambiguous words now sit only in the rule where an auxiliary settles the reading
("was split"), and the three noun spellings are keepers in the fixture.

**A known gap, stated rather than guessed at**: bare past-tense narration with no other tell ("section 6 listed the syscalls that fell
through") is not matched. A general past-tense rule would fire on legitimate description of current behavior, and this plugin refuses at write
time, where that trade goes the wrong way. The volume cap covers the same paragraph in source; in a document it does not.

Every failure path allows the call: an unparseable payload, an unparseable `tool_input`, an unjudged path, a tool that does not write files,
and any `hook_event_name` other than `PreToolUse`. The matcher is `*` and the tool name is filtered in Go, so the registration does not depend
on matcher-regex semantics.

- **Hook binary**: `plugins/no-tombstones/hook.go` -- the PreToolUse payload, the three write shapes, the source-only volume cap, the refusal
  text and its truncation notice
- **Detection**: `plugins/no-tombstones/tells.go` -- the tell table, the shared-order rule, the volume cap, and per-line-per-tell dedupe
- **Extraction**: `plugins/no-tombstones/comments.go` -- the per-language comment scanner, block merging, and the document prose walker
- **Referents**: `plugins/no-tombstones/referents.go` -- identifier eligibility, the bounded ripgrep probe, and the working-tree walk
- **Relocation**: `plugins/no-tombstones/relocate.go` -- the append to `.git/TOMBSTONES`
- **Tests**: `plugins/no-tombstones/tells_test.go` (the release-note block split into the sentences that must be refused, each by a NAMED
  tell, and the sentences that must survive), `hook_test.go` (every write shape with its unjudged-path control, the volume cap and its env
  override, relocation into a real repository, the dead-referent tier with a live-symbol control, and every fail-open path)
- **Plugin config**: `plugins/no-tombstones/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`

The prose half is `claude_snippets/no-history-in-comments.md` and `claude_snippets/comments-not-essays.md` in `PazerOP/claude-code-web-config`,
which cover what this hook cannot see: the same narration in a pull request body or a chat reply. Keep the two in sync if either changes.
