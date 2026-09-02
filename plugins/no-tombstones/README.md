# no-tombstones

Refuses a write that adds a tombstone comment: prose about a state the code is
no longer in, or an argument aimed at whoever reviews the diff.

```
blocked: this write adds a tombstone comment to internal/darwin/syscall.go.

  a date: "2026-09-02"
      // 2026-09-02: the macOS metadata wave
  a then-and-now contrast: "emulated now"
      // all of them are emulated now

The refused lines are appended to /repo/.git/TOMBSTONES. Nothing is lost:
put them in the commit message, where narrating a change belongs.
```

## What it looks for

A comment fails if its referent is gone (the flag, the test, the old spelling it
names is not in the tree) or its audience is the reviewer (it defends the change
instead of telling the next editor what breaks). Delete the sentence and ask
what the next editor now gets wrong; no answer means it was narration.

Three checks run, weakest first:

| Check | Catches |
|---|---|
| Tell table | dates, change references, former-state and contrast markers, addresses to the reviewer, defences of the change, quoted instructions, reported experiments |
| Volume cap | the essay whose every sentence reads as current -- source only |
| Dead referent | a name the repository does not define, with no tell in the wording |

## What it leaves alone

Code (only comment text is read, so a phrase inside a string literal is data),
fenced and indented blocks in a document, inline backtick spans, a file whose
extension it does not know, and everything a write does not add -- a tombstone
already in a file never blocks an unrelated edit to it.

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
