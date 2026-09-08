#!/bin/sh
# The turn-ending-with-the-work-undone guard. The rule is slopfix's `laziness`
# package, reached through `slopfix message`.
#
# This is the one guard here that stays a Stop hook. The siblings judge a
# message for its wording and annotate what the reader sees, because the message
# has already streamed and refusing cannot unsend it. This one exists to stop
# the model STOPPING, and only a Stop hook does that. An annotation under an
# abandoned turn changes nothing about the turn being abandoned.
#
# `slopfix message` reads the message itself on stdin, not a hook payload, so
# the message is lifted out of the payload here.
#
# The answer is one word: what the reader would have typed back. The lecture it
# replaced ran four paragraphs and argued its own case, which handed the model a
# case to argue with and spent the turn on that instead of the work.
set -eu

payload=$(cat)
message=$(printf '%s' "$payload" | jq -r 'select(.hook_event_name == "Stop") | .last_assistant_message // ""')
[ -n "$message" ] || exit 0

# Only exit 1 means findings. Any other failure is this guard breaking rather
# than the message offending, and a broken guard must not end the turn.
set +e
printf '%s' "$message" | "$(dirname "$0")/../bin/slopfix.ape" message >/dev/null 2>&1
status=$?
set -e
[ "$status" -eq 1 ] || exit 0

printf 'continue' >&2
exit 2
