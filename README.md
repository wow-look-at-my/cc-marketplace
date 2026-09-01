# no-blame-language

A turn does not end while its closing message deflects a defect instead of fixing it.

"Pre-existing", "not my problem", "out of scope", "flagging this for you", "that predates this session": each one reports a finding and stops
there, or shifts blame onto some other author or an earlier point in time. This org's convention bans that shape of sentence -- found it, fix
it, or say precisely why you are not the one to fix it. The Stop hook refuses the stop and quotes the phrase back.

```
Do not stop here. This message reports a defect instead of owning it:

  "pre-existing"
      That bug is pre-existing, so it stays left as-is.
  "left as-is"
      That bug is pre-existing, so it stays left as-is.

This org bans deflecting, blame-shifting language in a closing message: a
finding you report and leave is a defect you caused, and a correction never
authorizes naming who wrote the broken line first. Fix the root cause and say
so, or state precisely what you found, why it is not yours to fix, and what
you did instead -- never park a finding and walk away from it.

Rewrite your message without the phrase above, then stop.
```

## Fix it, or own the deferral

A genuine deferral is not banned -- naming a real blocker plainly, without deflecting language, is fine:

> This needs your call on A vs B, so I pushed the branch with A and left the test red.

What is banned is reporting a defect and stopping there, or reaching for provenance ("that predates this session", "this was existing code",
"git blame shows") to explain why it is someone else's problem.

## How it decides

Every banned phrase is matched case-insensitively against the message, with runs of whitespace collapsed to one space first, so a phrase a
markdown line-wrap split across two lines still matches. Fenced code, indented code and blockquotes are exempt, so documenting the rule does not
trip it. Inline backticks are not exempt.

## Install

```sh
claude plugin marketplace add wow-look-at-my/cc-marketplace
claude plugin install no-blame-language
```
