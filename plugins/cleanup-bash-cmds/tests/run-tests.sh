#!/usr/bin/env bash
# Self-contained tests for the cleanup-bash-cmds PreToolUse hook.
# Feeds synthetic hook payloads to hook.sh and asserts on the JSON output
# with jq. Prints PASS/FAIL per case and exits nonzero on any failure.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
HOOK=$SCRIPT_DIR/../hook.sh

PASS=0
FAIL=0

ok() {
	PASS=$((PASS + 1))
	printf 'PASS %s\n' "$1"
}

bad() {
	FAIL=$((FAIL + 1))
	printf 'FAIL %s: %s\n' "$1" "$2"
}

# Runs the hook with $1 on stdin; sets OUT and STATUS.
run_hook() {
	set +e
	OUT=$(printf '%s' "$1" | "$HOOK")
	STATUS=$?
	set -e
}

# Builds a minimal Bash PreToolUse payload for command $1.
payload_bash() {
	jq -cn --arg cmd "$1" '{hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: {command: $cmd}}'
}

# Asserts the hook rewrites command $2 to exactly $3.
check_rewrite() {
	local name=$1 in_cmd=$2 want=$3 got
	run_hook "$(payload_bash "$in_cmd")"
	if [ "$STATUS" -ne 0 ]; then
		bad "$name" "exit status $STATUS"
		return 0
	fi
	if [ -z "$OUT" ]; then
		bad "$name" "expected a rewrite, hook emitted nothing"
		return 0
	fi
	got=$(printf '%s' "$OUT" | jq -r '.hookSpecificOutput.updatedInput.command')
	if [ "$got" = "$want" ]; then
		ok "$name"
	else
		bad "$name" "got $(printf '%q' "$got"), want $(printf '%q' "$want")"
	fi
}

# Asserts the hook leaves command $2 alone (no output, exit 0).
check_noop() {
	local name=$1 in_cmd=$2
	run_hook "$(payload_bash "$in_cmd")"
	if [ "$STATUS" -eq 0 ] && [ -z "$OUT" ]; then
		ok "$name"
	else
		bad "$name" "expected exit 0 and no output, got status=$STATUS out=$(printf '%q' "$OUT")"
	fi
}

if [ -x "$HOOK" ]; then
	ok "hook script is executable"
else
	bad "hook script is executable" "$HOOK is not executable"
fi

# --- New rule: scrub stderr-to-/dev/null anywhere in the command ---

check_rewrite "simple trailing scrub" \
	'ls /nope 2>/dev/null' \
	'ls /nope'

# Mid-command occurrences leave a doubled blank behind; that is harmless to
# bash and accepted by design.
check_rewrite "mid-command scrub before &&" \
	'grep foo file 2>/dev/null && echo found' \
	'grep foo file  && echo found'

check_rewrite "all variants in one command" \
	'a 2>/dev/null; b 2> /dev/null; c 2>>/dev/null; d 2>> /dev/null; e 2>'\''/dev/null'\''; f 2>"/dev/null"' \
	'a ; b ; c ; d ; e ; f'

check_rewrite "adjacent pipe after scrub" \
	'foo 2>/dev/null|bar' \
	'foo |bar'

check_rewrite "scrub between pipeline stages" \
	'cmd 2>/dev/null | wc' \
	'cmd  | wc'

check_noop "multi-digit fd 12>/dev/null untouched" \
	'foo 12>/dev/null'

check_rewrite "fd 12 kept while fd 2 scrubbed" \
	'foo 12>/dev/null 2>/dev/null' \
	'foo 12>/dev/null'

check_rewrite "scrub inside a quoted string (blunt by design)" \
	'echo "silence with 2>/dev/null here"' \
	'echo "silence with  here"'

check_rewrite "scrub inside a heredoc (blunt by design)" \
	$'cat <<EOF\nsome 2>/dev/null text\nEOF' \
	$'cat <<EOF\nsome  text\nEOF'

# A scrub at end-of-line leaves the same harmless residual blank as a
# mid-command scrub (only string-leading/trailing whitespace is trimmed).
check_rewrite "multi-line command scrubbed per occurrence" \
	$'ls /a 2>/dev/null\nls /b 2>/dev/null' \
	$'ls /a \nls /b'

check_rewrite "redirect at start of command" \
	'2>/dev/null ls' \
	'ls'

# --- Non-goals: these stderr/stdout forms are NOT scrubbed ---

check_noop "stdout >/dev/null untouched" \
	'ls >/dev/null'

check_noop "&>/dev/null untouched" \
	'noisy &>/dev/null'

check_noop "spaced fd (2 >/dev/null) is a stdout redirect, untouched" \
	'echo 2 >/dev/null'

check_noop "distinct path /dev/null2 untouched" \
	'cmd 2>/dev/null2'

check_noop "distinct path /dev/null.log untouched" \
	'cmd 2>/dev/null.log'

check_noop "bare mid-command 2>&1 untouched" \
	'cmd 2>&1 | wc'

# ">/dev/null 2>&1": the >/dev/null part survives; the trailing 2>&1 is
# removed by the ported legacy rule.
check_rewrite "trailing 2>&1 removed but >/dev/null kept" \
	'foo >/dev/null 2>&1' \
	'foo >/dev/null'

# --- Ported legacy rules (1:1 with the Go hook_test.go cases) ---

check_rewrite "trailing 2>&1" 'ls -la 2>&1' 'ls -la'
check_rewrite "trailing || true" 'rm -f foo || true' 'rm -f foo'
check_rewrite "leading set -e;" 'set -e; npm test' 'npm test'
check_rewrite "leading set -e &&" 'set -e && npm test' 'npm test'
check_rewrite "whitespace trim" '  ls -la  ' 'ls -la'
check_rewrite "trailing | head" 'cat file.txt | head' 'cat file.txt'
check_rewrite "trailing | head -5" 'cat file.txt | head -5' 'cat file.txt'
check_rewrite "trailing | head -n 20" 'cat file.txt | head -n 20' 'cat file.txt'
check_rewrite "trailing | tail" 'cat file.txt | tail' 'cat file.txt'
check_rewrite "trailing | tail -10" 'cat file.txt | tail -10' 'cat file.txt'
check_rewrite "trailing | tail -n +5" 'cat file.txt | tail -n +5' 'cat file.txt'
check_rewrite "trailing | grep foo" 'cat file.txt | grep foo' 'cat file.txt'
check_rewrite "trailing | grep -i foo" 'cat file.txt | grep -i foo' 'cat file.txt'
check_rewrite "trailing | grep -v -i foo" 'cat file.txt | grep -v -i foo' 'cat file.txt'
check_rewrite "trailing | grep -A 3 foo" 'cat file.txt | grep -A 3 foo' 'cat file.txt'
check_rewrite "trailing | grep -E pattern" 'cat file.txt | grep -E pattern' 'cat file.txt'
check_rewrite "grep after 2>&1" 'ls -la 2>&1 | grep foo' 'ls -la'
check_rewrite "grep then head chain" 'cat file.txt | grep foo | head -5' 'cat file.txt'
check_rewrite "set -e with 2>&1" 'set -e; ls -la 2>&1' 'ls -la'
check_rewrite "2>&1 with || true" 'cmd 2>&1 || true' 'cmd'
check_rewrite "all legacy patterns" 'set -e; npm install 2>&1 || true' 'npm install'
check_rewrite "head after 2>&1" 'ls -la 2>&1 | head -20' 'ls -la'
check_rewrite "tail then grep chain" 'cmd | tail -10 | grep foo' 'cmd'

# --- Trailing | head / | tail: arbitrary flags, chains, word boundaries ---

check_rewrite "trailing |head without spaces" 'cat file.txt|head -50' 'cat file.txt'
check_rewrite "trailing | head -c 4k" 'cat file.txt | head -c 4k' 'cat file.txt'
check_rewrite "trailing | tail -f" 'cat /var/log/syslog | tail -f' 'cat /var/log/syslog'
check_rewrite "trailing | tail -n +2" 'cat file.txt | tail -n +2' 'cat file.txt'
check_rewrite "head then tail chain unwinds fully" 'cmd | head -5 | tail -2' 'cmd'
check_rewrite "deep alternating head/tail chain unwinds fully" \
	'cmd | head -9 | tail -8 | head -7 | tail -6 | head -5 | tail -4 | head -3 | tail -2 | head -1 | tail -1 | head -2 | tail -3' \
	'cmd'
check_rewrite "deep same-filter chain unwinds fully" \
	'cmd | head -1 | head -1 | head -1 | head -1 | head -1 | head -1 | head -1 | head -1 | head -1 | head -1 | head -1 | head -1' \
	'cmd'
check_rewrite "grep-interleaved chain unwinds via iteration" \
	'cmd | head -5 | grep x | tail -2' \
	'cmd'

check_noop "| headache untouched (word boundary)" 'cmd | headache'
check_noop "| tailscale untouched (word boundary)" 'cmd | tailscale status'
check_noop "| head5 untouched (word boundary)" 'cmd | head5'

# Multi-line: trailing means the end of the whole command string (same
# anchoring as every legacy trailing rule). A | head at the end of an
# earlier line is not trailing, and its argument matching cannot swallow
# the following lines.
check_noop "head at non-final line end untouched" $'foo | head -3\necho done'
check_rewrite "tail on final line is trailing" $'foo\nbar | tail -5' $'foo\nbar'

# Blunt-by-design caveat: a trailing filter inside a quoted string that
# reaches the end of the command is still stripped (the closing quote gets
# eaten as part of the argument token). Same stance as the /dev/null scrub.
check_rewrite "trailing | head inside quotes is stripped (blunt by design)" \
	"echo 'try: cmd | head -3'" \
	"echo 'try: cmd"

check_noop "|| true mid-command untouched" 'cmd || true && echo done'
check_noop "head mid-pipeline untouched" 'cmd | head -5 | wc'
check_noop "tail mid-pipeline untouched" 'cmd | tail -10 | wc'
check_noop "grep mid-pipeline untouched" 'cmd | grep foo | wc'
check_noop "grep with quoted pipe arg untouched" 'cmd | grep "foo|bar"'
check_noop "clean command passes through" 'git status'

# --- Payload handling and fail-open behavior ---

run_hook 'not json'
if [ "$STATUS" -eq 0 ] && [ -z "$OUT" ]; then
	ok "invalid JSON fails open"
else
	bad "invalid JSON fails open" "status=$STATUS out=$(printf '%q' "$OUT")"
fi

run_hook ''
if [ "$STATUS" -eq 0 ] && [ -z "$OUT" ]; then
	ok "empty stdin fails open"
else
	bad "empty stdin fails open" "status=$STATUS out=$(printf '%q' "$OUT")"
fi

run_hook "$(jq -cn '{hook_event_name: "PreToolUse", tool_name: "Read", tool_input: {command: "ls 2>/dev/null"}}')"
if [ "$STATUS" -eq 0 ] && [ -z "$OUT" ]; then
	ok "non-Bash tool passes through"
else
	bad "non-Bash tool passes through" "status=$STATUS out=$(printf '%q' "$OUT")"
fi

run_hook "$(jq -cn '{hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: {}}')"
if [ "$STATUS" -eq 0 ] && [ -z "$OUT" ]; then
	ok "missing command passes through"
else
	bad "missing command passes through" "status=$STATUS out=$(printf '%q' "$OUT")"
fi

# jq missing: PATH with bash but no jq must fail open before reading stdin.
FAKEBIN=$(mktemp -d)
ln -s "$(command -v bash)" "$FAKEBIN/bash"
set +e
NOJQ_OUT=$(printf '%s' "$(payload_bash 'ls 2>/dev/null')" | env PATH="$FAKEBIN" "$HOOK")
NOJQ_STATUS=$?
set -e
rm -rf "$FAKEBIN"
if [ "$NOJQ_STATUS" -eq 0 ] && [ -z "$NOJQ_OUT" ]; then
	ok "missing jq fails open"
else
	bad "missing jq fails open" "status=$NOJQ_STATUS out=$(printf '%q' "$NOJQ_OUT")"
fi

# --- Output JSON shape (verified schema, claude-code 2.1.201) ---

run_hook "$(payload_bash 'ls -la 2>&1')"
if [ "$STATUS" -eq 0 ] && printf '%s' "$OUT" | jq -e '
	(.hookSpecificOutput.hookEventName == "PreToolUse") and
	(.hookSpecificOutput | has("permissionDecision") | not) and
	(.hookSpecificOutput.updatedInput | type == "object") and
	(.hookSpecificOutput.additionalContext | type == "string") and
	(.hookSpecificOutput.additionalContext | contains("ls -la")) and
	(.systemMessage | type == "string")
' >/dev/null; then
	ok "output shape matches verified schema (no permissionDecision)"
else
	bad "output shape matches verified schema (no permissionDecision)" "out=$(printf '%q' "$OUT")"
fi

run_hook "$(payload_bash 'ls -la 2>&1')"
if [ "$(printf '%s' "$OUT" | wc -l)" -le 1 ]; then
	ok "output is a single compact JSON line"
else
	bad "output is a single compact JSON line" "out=$(printf '%q' "$OUT")"
fi

# --- updatedInput preserves every other tool_input field ---

EXTRA_PAYLOAD=$(jq -cn '{hook_event_name: "PreToolUse", tool_name: "Bash", tool_input: {command: "npm install 2>&1", timeout: 120000, description: "Install deps", run_in_background: false, custom_field: "keep-me"}}')
run_hook "$EXTRA_PAYLOAD"
if [ "$STATUS" -eq 0 ] && printf '%s' "$OUT" | jq -e '
	.hookSpecificOutput.updatedInput == {
		command: "npm install",
		timeout: 120000,
		description: "Install deps",
		run_in_background: false,
		custom_field: "keep-me"
	}
' >/dev/null; then
	ok "updatedInput preserves extra tool_input fields"
else
	bad "updatedInput preserves extra tool_input fields" "out=$(printf '%q' "$OUT")"
fi

# --- Rewrite logging (CLEANUP_BASH_CMDS_LOG) ---

LOGDIR=$(mktemp -d)
LOGFILE=$LOGDIR/hook.log

OUT=$(printf '%s' "$(payload_bash 'ls | grep foo')" | CLEANUP_BASH_CMDS_LOG="$LOGFILE" "$HOOK")
if [ -f "$LOGFILE" ] && grep -q 'original="ls | grep foo"' "$LOGFILE" && grep -q 'cleaned="ls"' "$LOGFILE"; then
	ok "log records original and cleaned command"
else
	bad "log records original and cleaned command" "log=$(cat "$LOGFILE" || printf 'missing')"
fi

OUT=$(printf '%s' "$(payload_bash 'git log | head -5')" | CLEANUP_BASH_CMDS_LOG="$LOGFILE" "$HOOK")
if [ "$(grep -c '^REWRITE	' "$LOGFILE")" -eq 2 ]; then
	ok "log appends across invocations"
else
	bad "log appends across invocations" "log=$(cat "$LOGFILE" || printf 'missing')"
fi

rm -f "$LOGFILE"
OUT=$(printf '%s' "$(payload_bash 'git status')" | CLEANUP_BASH_CMDS_LOG="$LOGFILE" "$HOOK")
if [ ! -e "$LOGFILE" ]; then
	ok "no log entry when nothing changes"
else
	bad "no log entry when nothing changes" "log=$(cat "$LOGFILE")"
fi

OUT=$(printf '%s' "$(payload_bash 'ls | grep foo')" | CLEANUP_BASH_CMDS_LOG="" "$HOOK")
if [ ! -e "$LOGFILE" ]; then
	ok "no log file when env var is empty"
else
	bad "no log file when env var is empty" "log=$(cat "$LOGFILE")"
fi

rm -rf "$LOGDIR"

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
