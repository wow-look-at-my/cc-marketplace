#!/usr/bin/env bash
# haiku-compact: switch the active Claude Code model to Haiku during context
# compaction (PreCompact) and restore the original model afterwards (PostCompact).
#
# Mutates ~/.claude/settings.json. Original model is stashed in a per-session
# sentinel file so PostCompact can restore it. All errors are logged to stderr
# and exit 0 — a buggy hook must never break compaction.

set -u
umask 077

fail() { echo "haiku-compact: $*" >&2; exit 0; }

command -v jq >/dev/null 2>&1 || fail "jq not found; skipping"

input=$(cat)
event=$(jq -r '.hook_event_name // ""' <<<"$input") || fail "bad stdin JSON"
session_id=$(jq -r '.session_id // ""' <<<"$input")
[ -n "$session_id" ] || fail "no session_id in input"

settings="$HOME/.claude/settings.json"
sentinel="${TMPDIR:-/tmp}/haiku-compact-${session_id}.json"

atomic_write() {
	local target=$1 content=$2 tmp
	tmp=$(mktemp "${target}.XXXXXX") || fail "mktemp failed for $target"
	printf '%s\n' "$content" >"$tmp" || { rm -f "$tmp"; fail "write failed for $target"; }
	mv "$tmp" "$target" || { rm -f "$tmp"; fail "rename failed for $target"; }
}

ensure_settings() {
	mkdir -p "$(dirname "$settings")" || fail "mkdir $(dirname "$settings") failed"
	[ -f "$settings" ] || atomic_write "$settings" '{}'
}

case "$event" in
	PreCompact)
		ensure_settings
		saved=$(jq -c '{model: .model, was_unset: (has("model") | not)}' "$settings") \
			|| fail "could not read $settings"
		atomic_write "$sentinel" "$saved"
		new=$(jq '.model = "haiku"' "$settings") || fail "jq set haiku failed"
		atomic_write "$settings" "$new"
		;;
	PostCompact)
		[ -f "$sentinel" ] || exit 0
		ensure_settings
		was_unset=$(jq -r '.was_unset' "$sentinel")
		if [ "$was_unset" = "true" ]; then
			new=$(jq 'del(.model)' "$settings") || fail "jq del failed"
		else
			model=$(jq -c '.model' "$sentinel")
			new=$(jq --argjson m "$model" '.model = $m' "$settings") || fail "jq restore failed"
		fi
		atomic_write "$settings" "$new"
		rm -f "$sentinel"
		;;
	*)
		exit 0
		;;
esac
