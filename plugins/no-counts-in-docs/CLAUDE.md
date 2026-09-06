## No Counts In Docs Plugin

The no-counts-in-docs plugin lives at `plugins/no-counts-in-docs/`. **It repairs rather than refuses.** The cardinal is cut out of the write. The write then goes through with `hookSpecificOutput.updatedInput`. `additionalContext` names each phrase it changed and the shape to write instead. There is no `permissionDecision` at all. The normal permission flow still decides the repaired write.

That is the standing rule in this marketplace. A guard that already knows the answer must not spend a round trip asking for it. A count is wrong because the number is there. Deleting the number is therefore the complete fix. It needs no judgement. `there are three sections` becomes `there are sections`, which stays true after the next commit. The old refusal cost a round trip per number. It also taught sessions to delete whole passages rather than reword them.

`StripCounts` cuts back to front, so an earlier span's offsets stay valid. `prose` carries each line's byte offset, so a phrase found on a line can be cut out of the document itself. The whole `tool_input` is carried through as a map. A key this plugin does not read therefore survives the rewrite.

The owner's ruling was that maintaining a count in a markdown file is not the kind of work worth doing at all. This is the same rule go-toolchain's own analyzer enforces on Go comments ("a number in a comment is a count of what exists today, and the edit that adds an item leaves it wrong"), applied to the documents the analyzer never reads.

**The rule is not here. It is slopfmt's.** This hook pipes each text a write adds through `slopfmt counts --json` and splices the answer back into the payload. It holds no pattern, no frame table and no splitter of its own. CI, an editor and this hook all shell out to the same binary, so none of them can drift from the others. `SLOPFMT` names another path, which is how the tests drive a stub instead.

A missing binary, a timeout and an unreadable answer all let the write through unchanged. A guard that mangles a write because its tool is absent is worse than no guard.

What stays here is the plumbing. Which writes are worth a subprocess (a markdown path, a write tool, a PreToolUse event), how the answer is spliced back, and the notice. The whole `tool_input` is carried through as a map, so a key this hook does not read survives the rewrite.

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

Every failure path lets the write through unchanged: an unparseable payload, an unparseable `tool_input`, a non-markdown path. A tool that does not write files, and any `hook_event_name` other than `PreToolUse`. The matcher is `*` and the tool name is filtered in Go. The registration does not depend on matcher-regex semantics.

- **Hook binary**: `plugins/no-counts-in-docs/hook.go` -- the PreToolUse payload, the write shapes, the call into `slopfmt counts --json`, and the notice. This is the whole plugin
- **Detection**: `wow-look-at-my/slopfmt`, package `counts`. The pattern, the measure and stop-word filters, and `Strip` live there. The exemptions come from slopfmt's own markdown splitter
- **Tests**: `plugins/no-counts-in-docs/hook_test.go`. The suite drives a stub in place of the binary, because the rule is tested where it lives. Covered here is the plumbing. Every write shape, and a non-markdown path as the negative control. Then the keys this hook does not read surviving, and a missing or unreadable tool letting the write through
- **Plugin config**: `plugins/no-counts-in-docs/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`
