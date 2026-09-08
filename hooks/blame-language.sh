#!/bin/sh
# The deflection guard. The rule is slopfix's `blamelanguage` package, reached
# through `slopfix message --only blame`.
#
# MessageDisplay, not Stop. A Stop hook runs after the message has streamed, so
# refusing cannot unsend it: the reader sees the deflection, then a near
# identical retype that explains itself and names the phrase again, and the
# guard fires a second time. That loop has no bound. The reader is the surface
# this rule is about, so the annotation goes there and the model is left alone.
#
# The message is judged whole on its last flush. One flush carries only the
# lines that completed since the last one, so a phrase straddling a wrap is
# missed and the same message is marked several times over. The deltas
# accumulate in a per-message file under the temp directory, keyed by
# message_id, and it is dropped on the final flush.
set -eu

payload=$(cat)
final=$(printf '%s' "$payload" | jq -r '.final // false')
id=$(printf '%s' "$payload" | jq -r '.message_id // ""')
delta=$(printf '%s' "$payload" | jq -r '.delta // ""')

[ "${CC_NO_BLAME_LANGUAGE:-1}" = "0" ] && exit 0
[ -n "$id" ] || exit 0

state="${TMPDIR:-/tmp}/slopfix-blame"
mkdir -p "$state" 2>/dev/null || exit 0
key="$state/$(printf '%s' "$id" | tr -c 'A-Za-z0-9._-' '_')"

printf '%s' "$delta" >>"$key" 2>/dev/null || exit 0
[ "$final" = "true" ] || exit 0

message=$(cat "$key" 2>/dev/null || printf '')
rm -f "$key"
[ -n "$message" ] || exit 0

set +e
findings=$(printf '%s' "$message" | "$(dirname "$0")/../bin/slopfix.ape" message --only blame --json 2>/dev/null)
set -e
[ -n "$findings" ] || exit 0

line=$(printf '%s' "$findings" | jq -r '
  (.findings // [])
  | map(.sentence)
  | unique
  | .[0:3] as $named
  | if ($named | length) == 0 then empty
    else "\n\n> **no-blame-language** -- "
       + ($named | map("\"" + . + "\"") | join(", "))
       + (if (.findings | length) > 3 then ", and more" else "" end)
       + ". Every repository here was written by the same hand, so there is no "
       + "other author to hand this to. Own it and say what you fixed."
    end') || exit 0
[ -n "$line" ] || exit 0

jq -n --arg text "$message$line" \
  '{hookSpecificOutput: {hookEventName: "MessageDisplay", displayContent: $text}}'
