#!/bin/sh
# Marks deflecting language. MessageDisplay: a Stop refusal only buys a retype.
set -eu

payload=$(cat)
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

set +e
findings=$(printf '%s' "$message" | "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" message --only blame --json 2>/dev/null)
set -e
[ -n "$findings" ] || exit 0

line=$(printf '%s' "$findings" | jq -r '
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

# Emit $delta: displayContent replaces the delta, so accumulation renders twice.
jq -n --arg text "$delta$line" \
  '{hookSpecificOutput: {hookEventName: "MessageDisplay", displayContent: $text}}'
