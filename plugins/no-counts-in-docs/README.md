# no-counts-in-docs

A write does not land while it states an inventory count in a markdown document.

`this repo's 15 plugins`, `the four rules below`, `it has three sections`: each says how many of something is here at the moment it was typed.
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
| `there are three sections` | `the sections are allow, ask and deny` |

Naming the things is better than counting them anyway: the reader learns what they are, and the sentence survives the next edit.

## What makes it a count

Two halves, and the second is what keeps this usable. The **frame** says the sentence is talking about what is here: a possessive
(`this repo's`), a having verb (`it ships`, `there are`, `the plugin registers`), or a deictic pointing into the page (`the rules below`). The
**quantity** is a cardinal governing a plural noun. Both together is an inventory the next commit falsifies.

A quantity with no frame is ordinary technical prose and passes: `pre-2.1.205 clients`, `ten diagnostics per file`, `the rule fired under five
selectors`. So does a measurement inside a frame — `20 seconds`, `500 lines`, `40000 characters`, `3 attempts` are limits and sizes, still true
after somebody adds a plugin.

Fenced code, indented code, HTML comments, YAML frontmatter and inline backtick spans are skipped whole: a number inside verbatim machinery is a
literal, not the document's own claim about itself. That is also how this page quotes the shape it refuses.

`one` is deliberately unmatched. In prose it is almost always a pronoun (`the wrong one`), and matching it would refuse far more good writing
than bad. That is a known gap, not an oversight.

Only the text a write ADDS is judged, so a count already sitting in a file never blocks an unrelated edit to it.
