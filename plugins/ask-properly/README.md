# ask-properly

A closing message that hands the user a decision in prose is marked where the reader can see it.

A question typed into a closing message is work pushed back onto the user. They have to reconstruct which options were meant and compose an answer. `AskUserQuestion` renders the choices instead, and writing them down forces the options to have been thought through. The hook appends one line to the message as it streams. It never blocks, and it sends nothing back to the model.

> Both documents state a rule and they disagree. Which rule should win?
>
> > **ask-properly** -- "Which rule should win?". That is a decision handed over in prose. Answer it yourself and say what you assumed, or ask it with AskUserQuestion: recommendation first and labelled, and every option saying what it costs and what it buys.

A refusal was the first design. It was wrong. A Stop hook runs after the message has streamed, so refusing cannot unsend it. The reader gets the prose question, then a near-identical retype that puts the decision back into prose while explaining itself. That fires the guard a second time.

## The way out

Either settle it yourself -- from the code, the docs, or a sensible default -- and say what you assumed. Or call `AskUserQuestion` with your recommendation first and labelled, each option describing what it costs and what it buys.

A message beside an `AskUserQuestion` card is never marked. Prose beside a rendered card is commentary, not an offloaded decision. The check is per turn: a call in an earlier turn does not license a prose question now. A card put up in the very message being displayed is not recorded yet. That one message can therefore still pick up the line.

A dismissed or unanswered card is not a ban on asking. Ask again, better.

## What it does not flag

A bare `?` is not enough on its own. Nullable types (`Int?`, `raw_args?`) and query strings (`.../compare/a...b?expand=1`) carry one and ask nothing.

Fenced code, indented code and blockquotes are exempt. A question can be quoted or documented. Inline backticks are not.

Reporting what you did and stopping is never marked. What earns the line is closing by inviting the user to decide.

The message is judged whole, on its last flush, because a question can span a line wrap. Set `CC_ASK_PROPERLY=0` to turn it off.

## Install

```sh
claude plugin marketplace add wow-look-at-my/cc-marketplace
claude plugin install ask-properly
```
