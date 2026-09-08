#!/bin/sh
# The two message checks that need the message assembled first. Every other
# check is a bare `slopfix.ape <subcommand>` in the manifest. $1 is the check.
set -eu

APE="${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape"

# Runs the check and prints its findings JSON, or nothing. Cobra returns 1 for
# an unknown subcommand too, so require JSON rather than an exit code.
findings() {
	set +e
	printf '%s' "$2" | "$APE" message --only "$1" --json 2>/dev/null
	set -e
}

payload=$(cat)

case "${1:?name a check: laziness or blame}" in
laziness)
	# One shot: a message that cannot be rewritten would trip this forever.
	[ "$(printf '%s' "$payload" | jq -r '.stop_hook_active // false')" = "true" ] && exit 0

	message=$(printf '%s' "$payload" | jq -r 'select(.hook_event_name == "Stop") | .last_assistant_message // ""')

	# The field is not always there. Without this fallback such a turn ends
	# unjudged, which is a guard that reports nothing on the payloads it is
	# handed rather than one that is off. The transcript is JSONL, newest
	# last, so take the last assistant entry.
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

	out=$(findings laziness "$message")
	[ -n "$out" ] || exit 0
	printf '%s' "$out" | jq -e '(.findings // []) | length > 0' >/dev/null 2>&1 || exit 0

	printf 'continue' >&2
	exit 2
	;;
blame)
	# MessageDisplay: a Stop refusal on this only buys a retype.
	final=$(printf '%s' "$payload" | jq -r '.final // false')
	id=$(printf '%s' "$payload" | jq -r '.message_id // ""')
	delta=$(printf '%s' "$payload" | jq -r '.delta // ""')

	# Four spellings: a one-value test breaks an opt-out silently.
	case "${CC_NO_BLAME_LANGUAGE:-1}" in
	0 | false | no | off) exit 0 ;;
	esac
	[ -n "$id" ] || exit 0

	state="${TMPDIR:-/tmp}/slopfix-blame"
	mkdir -p "$state" 2>/dev/null || exit 0
	key="$state/$(printf '%s' "$id" | tr -c 'A-Za-z0-9._-' '_')"

	printf '%s' "$delta" >>"$key" 2>/dev/null || exit 0
	[ "$final" = "true" ] || exit 0

	message=$(cat "$key" 2>/dev/null || printf '')
	rm -f "$key"

	# A message with no final flush strands its key. Swept here, not per delta.
	find "$state" -maxdepth 1 -type f -mmin +60 -delete 2>/dev/null || :

	[ -n "$message" ] || exit 0

	out=$(findings blame "$message")
	[ -n "$out" ] || exit 0

	line=$(printf '%s' "$out" | jq -r '
	  (.findings // []) as $f
	  | ($f | map(.phrase // .sentence) | unique | .[0:3]) as $named
	  | if ($named | length) == 0 then empty
	    else "\n\n> **no-blame-language** -- "
	       + ($named | map("\"" + . + "\"") | join(", "))
	       + (if ($f | length) > 3 then ", and more" else "" end)
	       + ". Every repository here was written by the same hand, so there is no "
	       + "other author to hand this to. Own it and say what you fixed."
	    end') || exit 0
	[ -n "$line" ] || exit 0

	# Emit $delta: displayContent replaces the delta, so accumulating renders twice.
	jq -n --arg text "$delta$line" \
	  '{hookSpecificOutput: {hookEventName: "MessageDisplay", displayContent: $text}}'
	;;
*)
	echo "unknown check: $1" >&2
	exit 1
	;;
esac
