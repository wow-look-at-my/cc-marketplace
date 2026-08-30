## No Counts In Docs Plugin

The no-counts-in-docs plugin lives at `plugins/no-counts-in-docs/`. It is one PreToolUse hook, modeled closely on the sibling
`no-blame-language` plugin: a `Write`, `Edit` or `MultiEdit` aimed at a markdown file is refused when the text it ADDS states a count. The
refusal is `permissionDecision: "deny"` with the offending phrase, the line it sits on, and the shape to write instead.

**The incident this exists for**: a session removed a plugin from the payload and dutifully edited `claude-code-web-config/CLAUDE.md` from "this
repo's 14 plugins" to "15 plugins", in the same turn it was declining to fix a red CI. The owner's ruling was that maintaining a count in a
markdown file is not the kind of work worth doing at all, because the count is wrong again the moment somebody adds a plugin and nothing in the
repository says so. This is the same rule go-toolchain's own analyzer enforces on Go comments ("a number in a comment is a count of what exists
today, and the edit that adds an item leaves it wrong"), applied to the documents the analyzer never reads.

**The shape it matches is a cardinal quantifying a plural noun**, not a number. `15 plugins`, `four rules`, `three sections`, and
`15 published marketplace plugins` (adjectives between the number and the noun are allowed) all match; a bare number does not. Two filters keep
this off ordinary technical prose, and both are load-bearing rather than defensive:

- **`measureNouns` excuses a measurement.** `20 seconds`, `500 lines`, `40000 characters`, `3 attempts` are limits, sizes and durations, all
  still true after somebody adds a plugin. Only a tally of what exists goes stale, and only a tally is refused.
- **`gapStopWords` excuses a function word between the number and the noun.** Without it, "Version 2 of the format drops the header" reads as a
  count of "drops", because a bare adjective run happily swallows "of the format". `TestOrdinaryProseWithoutACountIsLeftAlone` caught exactly
  that on the first run and is why the check exists.

**`one` is deliberately unmatched.** In English prose it is overwhelmingly a pronoun ("the wrong one", "one of them"), so matching it would
refuse far more good writing than bad. A document saying "one plugin" is also a document a single edit makes wrong, so this is a stated gap
rather than an oversight -- the same boundary `link-all-refs` draws around a bare `owner/repo`.

**Exemptions are ported from the sibling's `assertedText`, with one deliberate divergence.** Fenced code, indented code, HTML comments and YAML
frontmatter are skipped whole. Inline backtick spans ARE exempt here, where `no-blame-language` refuses to exempt them: a deflecting phrase in
backticks is still the writer's own voice, but a number inside verbatim machinery (`head -20 lines`) is a literal, not a claim about the
repository. Blanking a span keeps every byte offset, so the reported line still reads correctly.

**Only the text the write ADDS is judged** -- `content` for Write, `new_string` for Edit, every edit's `new_string` for MultiEdit. A count
already sitting in a file therefore never blocks an unrelated edit to it, which is what keeps the hook from holding a repository hostage to
prose nobody in this turn wrote.

Every failure path allows the call: an unparseable payload, an unparseable `tool_input`, a non-markdown path, a tool that does not write files,
and any `hook_event_name` other than `PreToolUse` -- a guard that blocks because it could not parse its own input is worse than no guard. The
matcher is `*` and the tool name is filtered in Go, so the registration does not depend on matcher-regex semantics.

- **Hook binary**: `plugins/no-counts-in-docs/hook.go` -- the PreToolUse payload, the three write shapes, the allow/deny decision, and the
  refusal text
- **Detection**: `plugins/no-counts-in-docs/counts.go` -- the cardinal-plus-plural pattern, the measure and stop-word filters, the
  fenced/indented/comment/frontmatter skip, inline-code blanking, and the markdown path test
- **Tests**: `plugins/no-counts-in-docs/counts_test.go`, `hook_test.go` -- a count in every form actually blocks, a measurement and ordinary
  prose do not, each exemption holds, prose after a closed fence is judged again, and every write shape is covered with the same text in a
  non-markdown file as its negative control
- **Plugin config**: `plugins/no-counts-in-docs/.claude-plugin/plugin.json` -- one PreToolUse hook registration on matcher `*`
