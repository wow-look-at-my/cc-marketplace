# no-blame-language

A closing message that deflects a defect instead of fixing it is marked where the reader can see it.

This org's convention bans that shape of sentence -- found it, fix it, or say precisely why you are not the one to fix it. The hook appends one line to the message as it streams. It never blocks, and it sends nothing back to the model.

> That bug is pre-existing, so it stays left as-is.
>
> > **no-blame-language** -- "pre-existing", "left as-is". This reports a defect instead of owning it. Fix the root cause and say so, or state precisely what you found, why it is not yours to fix, and what you did instead.

A refusal was the first design. It was wrong. A Stop hook runs after the message has streamed, so refusing cannot unsend it. The reader gets the deflection, then a near-identical retype that names the phrase again while explaining itself. That fires the guard a second time.

## Fix it, or own the deferral

A genuine deferral is not banned -- naming a real blocker plainly, without deflecting language, is fine:

> This needs your call on A vs B, so I pushed the branch with A and left the test red.

What is banned is reporting a defect and stopping there, or reaching for provenance ("that predates this session", "this was existing code", "git blame shows") to explain why it is someone else's problem.

## How it decides

Fenced code, indented code and blockquotes are exempt, so documenting the rule does not trip it. Inline backticks are not exempt.

The message is judged whole, on its last flush, because a phrase can span a line wrap. Set `CC_NO_BLAME_LANGUAGE=0` to turn it off.

## Install

```sh
claude plugin marketplace add wow-look-at-my/cc-marketplace
claude plugin install no-blame-language
```
