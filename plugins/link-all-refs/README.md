# link-all-refs

A pull request number, a commit SHA, a branch, a GitHub URL: each one becomes a markdown link as the message streams, so you can click it.

```
re-pushed as claude/fix-thing (6884dd2).
```

renders as

```
re-pushed as [claude/fix-thing](https://github.com/owner/repo/compare/master...claude/fix-thing?expand=1) ([6884dd2](https://github.com/owner/repo/commit/6884dd2)).
```

`[text](url)` is correct on both surfaces: the web client renders it as a link, and the terminal renders it as a real clickable hyperlink.

## It writes the link rather than asking for one

This used to refuse the end of the turn and make the model write the link. That cost a round trip every time, and the message written to comply names the reference again while explaining itself, so the guard fired again. The way out of that loop was a reply carrying nothing but links, which reads as an empty message.

A missing link is not work only a model can do: the token plus the checkout determine the URL. So the hook writes it. Nothing is sent back to the model and there is nothing to loop on.

## It will not give you a dead link

A link is a demand to stop reading and move your hand, and you pay that before you know whether it was worth paying. So a reference whose target cannot be shown to exist stays plain text:

- a branch is linked once it is on the remote — a branch that was never pushed has no compare page
- a commit is linked once the object is in the repository
- `owner/repo#42` needs no checkout at all, because it names its own repository

## How it decides

Text that is already a link is blanked before matching, so a correct reference is never rewritten twice and no link is nested inside another.

Fenced code, indented code and blockquotes are exempt, so documenting the rule does not trip it. Inline backticks are not exempt: a SHA in backticks is the case this exists to catch.

Every failure path renders the original text unchanged. `CC_LINK_ALL_REFS=0` turns it off.

## Install

```sh
claude plugin marketplace add wow-look-at-my/cc-marketplace
claude plugin install link-all-refs
```
