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

# One shot. A Stop refusal cannot unsend the message, so the model retypes, and
# a message it cannot rewrite into compliance would trip this guard forever.
# stop_hook_active is true on the continuation a refusal caused.
[ "$(printf '%s' "$payload" | jq -r '.stop_hook_active // false')" = "true" ] && exit 0

message=$(printf '%s' "$payload" | jq -r 'select(.hook_event_name == "Stop") | .last_assistant_message // ""')

# The field is not always there. Without this fallback such a turn ends
# unjudged, which is a guard that reports nothing on the payloads it is handed
# rather than one that is off. The transcript is JSONL, newest last, so take the
# last assistant entry.
if [ -z "$message" ]; then
	transcript=$(printf '%s' "$payload" | jq -r '.transcript_path // ""')
	if [ -n "$transcript" ] && [ -r "$transcript" ]; then
		message=$(jq -rs '
			map(select(.type == "assistant" or .role == "assistant"))
			| last
			| if . == null then ""
			  else (.message.content // .content) end
			| if type == "array" then map(select(.type == "text") | .text) | join("")
			  elif type == "string" then .
			  else "" end' "$transcript" 2>/dev/null || printf '')
	fi
fi
[ -n "$message" ] || exit 0

# The exit code alone cannot say whether the rule ran: cobra answers an unknown
# subcommand with 1, which is also what a finding returns. A binary too old to
# know `message` therefore refused every turn. So ask for JSON and require a
# finding in it. A broken guard must not end the turn.
set +e
findings=$(printf '%s' "$message" | "$(dirname "$0")/../bin/slopfix.ape" message --only laziness --json 2>/dev/null)
set -e
[ -n "$findings" ] || exit 0
printf '%s' "$findings" | jq -e '(.findings // []) | length > 0' >/dev/null 2>&1 || exit 0

printf 'continue' >&2
exit 2
