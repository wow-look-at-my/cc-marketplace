# no-tombstones

Strips a tombstone comment out of a write. A tombstone is prose about a state the code is no longer in, or an argument aimed at the reviewer. When the tombstone sits alone on one comment-only line, that line is deleted and the write proceeds. Only a finding that cannot be cleanly excised still gets the write refused.

A clean strip looks like this, reported once in `additionalContext`:

```
no-tombstones: removed 1 tombstone line(s) from internal/darwin/syscall.go before writing:
  // all of them are emulated now
```

A finding that cannot be cleanly excised is refused instead:

```
blocked: this write adds a tombstone comment to internal/darwin/syscall.go.

  a comment block of 20 lines: "// 2026-09-02: the macOS metadata wave"
      // 2026-09-02: the macOS metadata wave
```

## What it looks for

A comment fails if its referent is gone (the flag, the test, the old spelling it names is not in the tree) or its audience is the reviewer (it defends the change instead of telling the next editor what breaks). Delete the sentence and ask what the next editor now gets wrong. No answer means it was narration.

Three checks run, weakest first:

| Check | Catches |
|---|---|
| Tell table | dates, change references, former-state and contrast markers, addresses to the reviewer, defences of the change, quoted instructions, reported experiments |
| Volume cap | the essay whose every sentence reads as current -- source only |
| Dead referent | a name the repository does not define, with no tell in the wording |

## What it leaves alone

Code (only comment text is read, so a phrase inside a string literal is data), fenced and indented blocks in a document, inline backtick spans, a file whose extension it does not know. Everything a write does not add -- a tombstone already in a file never blocks an unrelated edit to it.

## What still gets refused instead of stripped

A finding is stripped only when it is the entire content of one raw source line. Three cases never qualify, so they still deny:

| Case | Why |
|---|---|
| The volume cap | a judgement that a whole block is too long, not a span to excise |
| A document sentence | a paragraph line commonly holds several sentences; deleting it risks a keeper |
| A tombstone sharing a line with code | a trailing `// comment`, or a block comment's open/close line carrying code |

## Configuration

| Variable | Default | Effect |
|---|---|---|
| `NO_TOMBSTONES_MAX_COMMENT_LINES` | `14` | Longest single comment block in source. `0` turns the cap off. |

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install no-tombstones
```

## License

MIT
