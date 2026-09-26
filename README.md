# no-repeat-reply

A Stop hook that breaks one livelock. A gate re-fires. The assistant answers with the message it just sent. The two then spin until the session dies without producing work.

When a turn's closing message matches the one before it, the hook refuses that stop once and hands back the checks to run instead. It never refuses twice in a session.

## What counts as a repeat

- The closing message of a turn. That is an assistant record with `stop_reason: "end_turn"` carrying a text block.
- Compared after whitespace and case collapse. A reworded repeat is not caught.
- At least 24 characters. "Done." twice is an acknowledgement.

## Install

```bash
claude plugin install no-repeat-reply
```

Everything fails open. An unparseable payload, a missing transcript and an unwritable marker directory all allow the stop.
