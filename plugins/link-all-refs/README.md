# link-all-refs

A turn does not end while its closing message names something the reader cannot click.

A pull request number, a commit SHA, a branch, a GitHub URL: each one is a markdown link, or the Stop hook refuses the stop and says which token
is bare.

```
Do not stop here. This message names things the user cannot click:

  6884dd2 -- a commit SHA
      re-pushed as `claude/binary-name-collision-drop` (`6884dd2`).
  claude/binary-name-collision-drop -- a branch
      re-pushed as `claude/binary-name-collision-drop` (`6884dd2`).
```

## Write the link

`[text](url)` is correct on both surfaces. The web client renders it as a link, and the terminal renders it as a real clickable hyperlink -- Claude
Code passes markdown links through the terminal's OSC 8 escape when the terminal advertises support.

```
[owner/repo#42](https://github.com/owner/repo/pull/42)
[6884dd2](https://github.com/owner/repo/commit/6884dd2)
[claude/fix-thing](https://github.com/owner/repo/compare/master...claude/fix-thing?expand=1)
```

## How it decides

Strip every markdown link out of the closing message, then look at what is left. Anything found was never linked. A link given earlier in the
message, or in an earlier turn, earns no credit -- the reader is looking at the text in front of them.

Fenced code, indented code and blockquotes are exempt, so documenting the rule does not trip it. Inline backticks are not exempt: a SHA in
backticks is the case this exists to catch.

## Install

```sh
claude plugin marketplace add wow-look-at-my/cc-marketplace
claude plugin install link-all-refs
```
