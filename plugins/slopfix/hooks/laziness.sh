#!/bin/sh
# Marks a message reporting a defect it did not fix. Stop, not MessageDisplay.
set -eu

payload=$(cat)

# One shot: a message that cannot be rewritten would trip this forever.
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

# Cobra returns 1 for an unknown subcommand too, so require JSON, not an exit.
set +e
findings=$(printf '%s' "$message" | "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" message --only laziness --json 2>/dev/null)
set -e
[ -n "$findings" ] || exit 0
printf '%s' "$findings" | jq -e '(.findings // []) | length > 0' >/dev/null 2>&1 || exit 0

printf 'continue' >&2
exit 2
