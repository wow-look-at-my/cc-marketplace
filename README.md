# ask-properly

A turn does not end while its closing message hands the user a decision in prose.

A question typed into a closing message is work pushed back onto the user: they have to reconstruct which options were
meant and compose an answer. `AskUserQuestion` renders the choices instead, and writing them down forces the options to
have been thought through. The Stop hook refuses the stop and quotes what it found:

```
Do not stop here. This message ends by handing the user a decision in prose:

  [question] "Which rule should win?"
      Both documents state a rule and they disagree. Which rule should win?
  [deferral] "your call"
      I can go either way here -- your call.

A question typed into a closing message is work pushed back onto the user:
they have to reconstruct the options and compose a reply. Two ways out, and
only two:

  1. ANSWER IT YOURSELF. ...
  2. ASK IT WITH AskUserQuestion. ...
```

## The way out

Either settle it yourself -- from the code, the docs, or a sensible default -- and say what you assumed; or call
`AskUserQuestion` with your recommendation first and labelled, each option describing what it costs and what it buys,
and every open question batched into one call.

A turn that called `AskUserQuestion` may end however it likes. Prose beside a rendered card is commentary, not an
offloaded decision. The check is per turn: a call in an earlier turn does not license a prose question now.

A dismissed or unanswered card is not a ban on asking. Ask again, better.

## What it does not flag

A bare `?` is not enough on its own. Nullable types (`Int?`, `raw_args?`) and query strings (`.../compare/a...b?expand=1`)
carry one and ask nothing, so links are stripped first and a question mark counts only when it ends its line or its
sentence opens with an interrogative cue.

Fenced code, indented code and blockquotes are exempt, so a question can be quoted or documented. Inline backticks are
not.

Reporting what you did and stopping is always allowed. What is banned is closing by inviting the user to decide.

## Install

```sh
claude plugin marketplace add wow-look-at-my/cc-marketplace
claude plugin install ask-properly
```
