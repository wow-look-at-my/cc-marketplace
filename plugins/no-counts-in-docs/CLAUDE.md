## No Counts In Docs Plugin

The no-counts-in-docs plugin lives at `plugins/no-counts-in-docs/`. **It repairs rather than refuses.** The cardinal is cut out of the write. The write then goes through with `hookSpecificOutput.updatedInput`. `additionalContext` names each phrase it changed and the shape to write instead. There is no `permissionDecision` at all. The normal permission flow still decides the repaired write.

That is the standing rule in this marketplace. A guard that already knows the answer must not spend a round trip asking for it. A count is wrong because the number is there. Deleting the number is therefore the complete fix. It needs no judgement. `there are three sections` becomes `there are sections`, which stays true after the next commit. The old refusal cost a round trip per number. It also taught sessions to delete whole passages rather than reword them.

`StripCounts` cuts back to front, so an earlier span's offsets stay valid. `prose` carries each line's byte offset, so a phrase found on a line can be cut out of the document itself. The whole `tool_input` is carried through as a map. A key this plugin does not read therefore survives the rewrite.

The owner's ruling was that maintaining a count in a markdown file is not the kind of work worth doing at all. This is the same rule go-toolchain's own analyzer enforces on Go comments ("a number in a comment is a count of what exists today, and the edit that adds an item leaves it wrong"), applied to the documents the analyzer never reads.

**The rule is slopfmt's, and so is the plumbing.** `slopfmt hook --only counts` reads the PreToolUse payload on stdin and writes the response on stdout. `updatedInput` carries the repaired payload and `additionalContext` names what went. CI, an editor and this hook all shell out to the same binary, so none of them can drift from the others.

That plumbing used to live here, in Go, and identically again in `no-tombstones`. The same payload parse, the same Write/Edit/MultiEdit shapes, the same splice back into `tool_input`. A write shape added to one and not the other is a guard that silently stops seeing half the writes.

**`hook.sh` exists for one reason: to fail OPEN.** A PreToolUse hook blocks the tool on any non-zero exit. Naming the binary straight in `plugin.json` turns one that cannot answer into a guard that refuses every write. The binary carries its verdict in the JSON it prints, and exits 0 whatever it decides. A non-zero exit therefore means it never ran. The launcher probes for the binary AND swallows a non-zero exit. Probing alone is not enough. An installed binary too old to know the `hook` subcommand exits 1. The `exec` form handed that straight to the tool as a refusal, and it shipped. `SLOPFMT` names another path, which is how a test drives a stub.

The rule slopfmt applies is described below, because a reader of this plugin needs to know what it asks for. `wow-look-at-my/slopfmt` is where it lives and where it is tested.

**A count needs a FRAME as well as a QUANTITY. The frame is the whole precision story.** The first draft matched a quantity alone. A PreToolUse deny that broad makes every existing doc unwritable, which is how a guard earns the reputation that uninstalls it. So a quantity only counts when a frame says the sentence is talking about what is HERE:

- **`possessiveFrame`** -- a determiner claiming the things belong here (`this repo's 15 plugins`, `the payload's four steps`).
- **`havingFrame`** -- a verb asserting possession or extent (`it ships two hooks`, `there are three sections`, `the plugin registers 15 servers`).
- **`deicticFrame`** -- a pointer into the page (`the four rules below`), where editing the page is what breaks the number.

Two filters then run inside the frame. **`measureNouns`** excuses a measurement: `20 seconds`, `500 lines`, `3 attempts` are limits and sizes, still true after somebody adds a plugin. **`gapStopWords`** excuses a function word between the cardinal and the noun. Without it `it has 2 of the format drops` reads as a count of `drops`. A bare adjective run happily swallows `of the format`.

**The digit guard lives in Go, not in the regex. That is not a style choice.** RE2 has no lookbehind. The frame can never meet the quantity -- every positive test went red at once. `continuesANumber` looks at the byte in front of the match instead.

**`one` is deliberately unmatched.** In English prose it is overwhelmingly a pronoun ("the wrong one", "one of them"), so matching it will refuse far more good writing than bad. A document saying "one plugin" is also a document a single edit makes wrong. This is a stated gap rather than an oversight -- the same boundary `link-all-refs` draws around a bare `owner/repo`.

**Exemptions are ported from the sibling's `assertedText`, with one deliberate divergence.** Fenced code, indented code, HTML comments and YAML frontmatter are skipped whole. Inline backtick spans ARE exempt here, where `no-blame-language` refuses to exempt them: a deflecting phrase in backticks is still the writer's own voice. Blanking a span keeps every byte offset, so the reported line still reads correctly.

**Only the text the write ADDS is judged** -- `content` for Write, `new_string` for Edit, every edit's `new_string` for MultiEdit. A count already sitting in a file therefore never blocks an unrelated edit to it.

Every failure path lets the write through unchanged: an unparseable payload, an unparseable `tool_input`, a non-markdown path. A tool that does not write files, and any `hook_event_name` other than `PreToolUse`. The matcher is `*` and the tool name is filtered inside slopfmt. The registration does not depend on matcher-regex semantics.

- **Launcher**: `plugins/no-counts-in-docs/hook.sh` -- the probe and the exec. This is the whole plugin
- **Rule and plumbing**: `wow-look-at-my/slopfmt`. Package `counts` holds the pattern, the filters and `Strip`. `cmd/hook.go` holds the payload, the write shapes and the response, and both are tested there
- **Plugin config**: `plugins/no-counts-in-docs/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`
