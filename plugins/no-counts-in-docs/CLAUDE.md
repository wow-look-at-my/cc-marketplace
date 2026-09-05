## No Counts In Docs Plugin

The no-counts-in-docs plugin lives at `plugins/no-counts-in-docs/`. The refusal is `permissionDecision: "deny"` with the offending phrase. The line it sits on, and the shape to write instead.

The owner's ruling was that maintaining a count in a markdown file is not the kind of work worth doing at all. This is the same rule go-toolchain's own analyzer enforces on Go comments ("a number in a comment is a count of what exists today, and the edit that adds an item leaves it wrong"), applied to the documents the analyzer never reads.

**A count needs a FRAME as well as a QUANTITY. The frame is the whole precision story.** The first draft matched a quantity alone. A PreToolUse deny that broad makes every existing doc unwritable, which is how a guard earns the reputation that uninstalls it. So a quantity only counts when a frame says the sentence is talking about what is HERE:

- **`possessiveFrame`** -- a determiner claiming the things belong here (`this repo's 15 plugins`, `the payload's four steps`).
- **`havingFrame`** -- a verb asserting possession or extent (`it ships two hooks`, `there are three sections`, `the plugin registers 15 servers`).
- **`deicticFrame`** -- a pointer into the page (`the four rules below`), where editing the page is what breaks the number.

Two filters then run inside the frame. **`measureNouns`** excuses a measurement: `20 seconds`, `500 lines`, `3 attempts` are limits and sizes, still true after somebody adds a plugin. **`gapStopWords`** excuses a function word between the cardinal and the noun -- without it `it has 2 of the format drops` reads as a count of `drops`, because a bare adjective run happily swallows `of the format`.

**The digit guard lives in Go, not in the regex. That is not a style choice.** RE2 has no lookbehind. The frame can never meet the quantity -- every positive test went red at once. `continuesANumber` looks at the byte in front of the match instead.

**`one` is deliberately unmatched.** In English prose it is overwhelmingly a pronoun ("the wrong one", "one of them"), so matching it will refuse far more good writing than bad. A document saying "one plugin" is also a document a single edit makes wrong. This is a stated gap rather than an oversight -- the same boundary `link-all-refs` draws around a bare `owner/repo`.

**Exemptions are ported from the sibling's `assertedText`, with one deliberate divergence.** Fenced code, indented code, HTML comments and YAML frontmatter are skipped whole. Inline backtick spans ARE exempt here, where `no-blame-language` refuses to exempt them: a deflecting phrase in backticks is still the writer's own voice. Blanking a span keeps every byte offset, so the reported line still reads correctly.

**Only the text the write ADDS is judged** -- `content` for Write, `new_string` for Edit, every edit's `new_string` for MultiEdit. A count already sitting in a file therefore never blocks an unrelated edit to it.

Every failure path allows the call: an unparseable payload, an unparseable `tool_input`, a non-markdown path. A tool that does not write files, and any `hook_event_name` other than `PreToolUse`. The matcher is `*` and the tool name is filtered in Go. The registration does not depend on matcher-regex semantics.

- **Hook binary**: `plugins/no-counts-in-docs/hook.go` -- the PreToolUse payload, the three write shapes, the allow/deny decision, and the refusal text
- **Detection**: `plugins/no-counts-in-docs/counts.go` -- the cardinal-plus-plural pattern, the measure and stop-word filters, the fenced/indented/comment/frontmatter skip, inline-code blanking, and the markdown path test
- **Tests**: `plugins/no-counts-in-docs/counts_test.go`, `hook_test.go` -- a count in every form actually blocks, a measurement and ordinary prose do not, each exemption holds, prose after a closed fence is judged again, and every write shape is covered with the same text in a non-markdown file as its negative control
- **Plugin config**: `plugins/no-counts-in-docs/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`
