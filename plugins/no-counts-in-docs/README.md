# no-counts-in-docs

A write does not land while it states a count in a markdown document.

"This repo's 15 plugins", "the four rules below", "it has three sections": each says how many of something exists at the moment it was typed.
The edit that adds an item leaves the number wrong, and nothing in the repository corrects it — the reader keeps trusting a figure that has
quietly gone stale. The PreToolUse hook refuses the write and quotes the count back.

```
blocked: this write states a count in /repo/CLAUDE.md.

  "15 plugins"
      The payload embeds this repo's 15 plugins.

A count is true only until somebody adds or removes an item, and nothing in the
repository corrects it when they do -- the reader keeps trusting a number that
has quietly gone wrong. Describe what is there and let the reader count:
"every plugin this repo installs", not "this repo's 15 plugins"; "the rules
below", not "the four rules below".

Rewrite the text without the count, then write the file.
```

## Write this instead

| Instead of | Write |
|---|---|
| `this repo's 15 plugins` | `every plugin this repo installs` |
| `the four rules below` | `the rules below` |
| `it answers two events` | `it answers PermissionRequest and PreToolUse` |

Naming the things is better than counting them anyway: the reader learns what they are, and the sentence survives the next edit.

## What it does not refuse

A measurement is not a count. `20 seconds`, `500 lines`, `40000 characters` and `3 attempts` are limits and sizes — still true after somebody
adds a plugin — so they pass. Fenced code, indented code, HTML comments, YAML frontmatter and inline backtick spans are skipped whole: a number
inside verbatim machinery is a literal, not the document's own claim about itself.

`one` is deliberately unmatched. In prose it is almost always a pronoun ("the wrong one"), and matching it would refuse far more good writing
than bad. That is a known gap, not an oversight.

Only the text a write ADDS is judged, so a count already sitting in a file never blocks an unrelated edit to it.
