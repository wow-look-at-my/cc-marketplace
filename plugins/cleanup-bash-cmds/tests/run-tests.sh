#!/usr/bin/env bash
# Self-contained tests for the cleanup-bash-cmds PreToolUse hook.
# Feeds synthetic hook payloads to hook.sh and asserts on the JSON output
# with jq. Prints PASS/FAIL per case and exits nonzero on any failure.
#
# The hook needs shfmt (--to-json/--from-json, v3.7.0+). If the environment
# lacks a suitable shfmt (e.g. a bare CI runner), a pinned release binary is
# bootstrapped into a temp dir for the duration of the run.
#
# shellcheck disable=SC2016  # test inputs are literal commands; $() in them
#                            # is data for the hook, never meant to expand

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
HOOK=$SCRIPT_DIR/../hook.sh

# Almost every rewritten command gains this prefix (pipefail injection).
PFX=$'set -o pipefail\n'

# The echo_nag replacement command (exact text, byte-for-byte).
NAG='echo "system message: do not use echo/printf to communicate with the user"'
# The same nag rendered with the printf command word (Args[0] is preserved).
NAG_PRINTF='printf "system message: do not use echo/printf to communicate with the user"'

if ! command -v shfmt >/dev/null || ! shfmt --help 2>&1 | grep -q -- '--from-json'; then
	SHFMT_VERSION=v3.8.0
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	arch=$(uname -m)
	case "$arch" in
	x86_64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	esac
	bindir=$(mktemp -d)
	url="https://github.com/mvdan/sh/releases/download/${SHFMT_VERSION}/shfmt_${SHFMT_VERSION}_${os}_${arch}"
	echo "shfmt with --from-json not found; bootstrapping ${SHFMT_VERSION}"
	curl -fsSL "$url" -o "$bindir/shfmt" ||
		gh release download "$SHFMT_VERSION" -R mvdan/sh \
			--pattern "shfmt_${SHFMT_VERSION}_${os}_${arch}" -O "$bindir/shfmt"
	chmod +x "$bindir/shfmt"
	export PATH="$bindir:$PATH"
fi
echo "using shfmt $(shfmt --version) at $(command -v shfmt)"

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

# Rewrite: command $2 becomes exactly $3. The hook is fully silent by
# design: EVERY rewrite must carry suppressOutput: true and must NOT carry
# a systemMessage, additionalContext, or permissionDecision.
check_rewrite_raw() {
	local name=$1 in_cmd=$2 want=$3 got
	run_hook "$(payload_bash "$in_cmd")"
	if [ "$STATUS" -ne 0 ] || [ -z "$OUT" ]; then
		bad "$name" "expected a rewrite, got status=$STATUS out=$(printf '%q' "$OUT")"
		return 0
	fi
	if ! printf '%s' "$OUT" | jq -e '
		(.hookSpecificOutput.hookEventName == "PreToolUse") and
		(.hookSpecificOutput | has("permissionDecision") | not) and
		(.suppressOutput == true) and
		(has("systemMessage") | not) and
		(.hookSpecificOutput | has("additionalContext") | not)
	' >/dev/null; then
		bad "$name" "rewrite must be silent; got $(printf '%q' "$OUT")"
		return 0
	fi
	got=$(printf '%s' "$OUT" | jq -r '.hookSpecificOutput.updatedInput.command')
	if [ "$got" = "$want" ]; then
		ok "$name"
	else
		bad "$name" "command: got $(printf '%q' "$got"), want $(printf '%q' "$want")"
	fi
}

# Rewrite whose result also gains the pipefail prefix (the common case).
check_rewrite() {
	local name=$1 in_cmd=$2 want=$3
	check_rewrite_raw "$name" "$in_cmd" "${PFX}${want}"
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

# Asserts the hook DENIES command $2: permissionDecision deny plus a reason
# pointing at the Write tool, but no updatedInput and no systemMessage.
check_deny() {
	local name=$1 in_cmd=$2
	run_hook "$(payload_bash "$in_cmd")"
	if [ "$STATUS" -eq 0 ] && [ -n "$OUT" ] && printf '%s' "$OUT" | jq -e '
		(.hookSpecificOutput.hookEventName == "PreToolUse") and
		(.hookSpecificOutput.permissionDecision == "deny") and
		(.hookSpecificOutput.permissionDecisionReason | test("Write")) and
		(.hookSpecificOutput | has("updatedInput") | not) and
		(has("systemMessage") | not)
	' >/dev/null; then
		ok "$name"
	else
		bad "$name" "expected silent deny JSON, got status=$STATUS out=$(printf '%q' "$OUT")"
	fi
}

if [ -x "$HOOK" ]; then
	ok "hook script is executable"
else
	bad "hook script is executable" "$HOOK is not executable"
fi

# --- Scrub stderr-to-/dev/null redirections (AST Redirect nodes only) ---

check_rewrite "simple trailing scrub" \
	'ls /nope 2>/dev/null' \
	'ls /nope'

check_rewrite "mid-command scrub before &&" \
	'grep foo file 2>/dev/null && cat found' \
	'grep foo file && cat found'

# Semicolon-separated statements come back one per line (shfmt-canonical).
check_rewrite "all redirect variants in one command" \
	'a 2>/dev/null; b 2> /dev/null; c 2>>/dev/null; d 2>> /dev/null; e 2>'\''/dev/null'\''; f 2>"/dev/null"' \
	$'a\nb\nc\nd\ne\nf'

check_rewrite "adjacent pipe after scrub (canonical spacing)" \
	'foo 2>/dev/null|bar' \
	'foo | bar'

check_rewrite "scrub between pipeline stages" \
	'cmd 2>/dev/null | wc' \
	'cmd | wc'

check_rewrite "multi-digit fd 12>/dev/null untouched" \
	'foo 12>/dev/null' \
	'foo 12>/dev/null'

check_rewrite "fd 12 kept while fd 2 scrubbed" \
	'foo 12>/dev/null 2>/dev/null' \
	'foo 12>/dev/null'

check_rewrite "redirect before the command word" \
	'2>/dev/null ls' \
	'ls'

check_rewrite "scrub inside a command substitution" \
	'echo $(ls /x 2>/dev/null)' \
	'echo $(ls /x)'

check_rewrite "multi-line command scrubbed per statement" \
	$'ls /a 2>/dev/null\nls /b 2>/dev/null' \
	$'ls /a\nls /b'

# AST-aware: text that merely CONTAINS the pattern is not a redirect.
check_rewrite "literal-string carrier containing 2>/dev/null untouched" \
	': "silence with 2>/dev/null here"' \
	': "silence with 2>/dev/null here"'

# --- Heredocs: banned outright (deny beats everything, incl. pipefail) ---

check_deny "plain heredoc denied" \
	$'cat <<EOF > /tmp/f\nhello\nEOF'

check_deny "dash heredoc (<<-) denied" \
	$'cat <<-EOF\n\thello\nEOF'

check_deny "quoted-delimiter heredoc denied" \
	$'cat <<\'EOF\'\nhello\nEOF'

check_deny "heredoc inside command substitution denied" \
	$'x=$(cat <<INNER\nhi\nINNER\n)'

check_deny "heredoc plus 2>/dev/null is denied, not rewritten" \
	$'cat <<EOF 2>/dev/null\nsome text\nEOF'

# Herestrings are not heredocs: allowed, and other cleanups still apply.
check_rewrite "herestring alone gets only pipefail" \
	'grep x <<<"data"' \
	'grep x <<<"data"'

check_rewrite "herestring kept while 2>/dev/null scrubbed" \
	'grep x <<<"data" 2>/dev/null' \
	'grep x <<<"data"'

check_rewrite "string literal containing <<EOF untouched" \
	': "here is <<EOF in a string"' \
	': "here is <<EOF in a string"'

# The arithmetic shift shares the << token number with heredocs in shfmt's
# typed JSON; the Redirs-scoped match must not deny it.
check_rewrite "arithmetic bit shift is not a heredoc" \
	'echo $((1 << 2))' \
	'echo $((1 << 2))'

# --- Non-goals: these redirect forms are NOT scrubbed ---

check_rewrite "stdout >/dev/null untouched by scrub and tee" \
	'ls >/dev/null' \
	'ls >/dev/null'

check_rewrite "&>/dev/null untouched" \
	'noisy &>/dev/null' \
	'noisy &>/dev/null'

check_rewrite "spaced fd (2 >/dev/null) is a stdout-to-/dev/null discard" \
	'echo 2 >/dev/null' \
	'echo 2 >/dev/null'

check_rewrite "distinct path /dev/null2 not scrubbed" \
	'cmd 2>/dev/null2' \
	'cmd 2>/dev/null2'

check_rewrite "distinct path /dev/null.log not scrubbed" \
	'cmd 2>/dev/null.log' \
	'cmd 2>/dev/null.log'

check_rewrite "bare mid-command 2>&1 untouched" \
	'cmd 2>&1 | wc' \
	'cmd 2>&1 | wc'

# ">/dev/null 2>&1": trailing 2>&1 stripped; the /dev/null discard stays a
# discard (tee exclusion).
check_rewrite "trailing 2>&1 stripped, >/dev/null kept" \
	'foo >/dev/null 2>&1' \
	'foo >/dev/null'

# --- Trailing stdout file redirects become | tee ---

check_rewrite "stdout > file becomes tee" \
	'cmd > f' \
	'cmd | tee f'

check_rewrite "stdout >> file becomes tee -a" \
	'cmd >> f' \
	'cmd | tee -a f'

check_rewrite "pipeline > file keeps its pipes" \
	'a | b > f' \
	'a | b | tee f'

check_rewrite "quoted expansion target preserved verbatim" \
	'cmd > "$OUT"' \
	'cmd | tee "$OUT"'

check_rewrite "prefix-position redirect rewritten" \
	'> f cmd' \
	'cmd | tee f'

check_rewrite "stdin redirect stays on the producer" \
	'cmd < in > out' \
	'cmd <in | tee out'

# Final-statement anchoring: only the rightmost && / || leaf is "trailing".
check_rewrite "tee not applied to a non-trailing && member" \
	'cmd > f && next' \
	'cmd >f && next'

check_rewrite "tee applies to the trailing && member" \
	'next && cmd > f' \
	'next && cmd | tee f'

check_rewrite "trailing 2>&1 stripped before tee" \
	'cmd > f 2>&1' \
	'cmd | tee f'

check_rewrite "devnull scrub composes with tee" \
	'x 2>/dev/null > f' \
	'x | tee f'

check_rewrite "a tee stage with its own stdout redirect chains" \
	'cmd | tee a > b' \
	'cmd | tee a | tee b'

# Exclusions: only pipefail is injected.
check_rewrite "stderr file redirect is not touched" \
	'cmd 2> err.log' \
	'cmd 2>err.log'

check_rewrite "tee not applied inside command substitution" \
	'VAR=$(cmd > f)' \
	'VAR=$(cmd >f)'

check_rewrite "double stdout redirect is skipped" \
	'cmd > a > b' \
	'cmd >a >b'

check_rewrite "process substitution target is skipped" \
	'cmd > >(gzip)' \
	'cmd > >(gzip)'

# --- set -o pipefail on every command ---

check_rewrite "bare command gets pipefail" \
	'git status' \
	'git status'

check_noop "set -o pipefail already present" 'set -o pipefail; ls'
check_noop "set -eo pipefail recognized" 'set -eo pipefail; ls'
check_noop "set -euo pipefail recognized (and never stripped)" 'set -euo pipefail; ls'
check_noop "set -e -o pipefail recognized" 'set -e -o pipefail; ls'
check_noop "multiple -o pairs recognized" 'set -o errexit -o pipefail; ls'
check_noop "set -o pipefail && chain recognized" 'set -o pipefail && ls'

check_rewrite_raw "pipefail not duplicated when other rules fire" \
	'set -o pipefail; ls -la 2>&1' \
	$'set -o pipefail\nls -la'

# Strictness settings are never removed.
check_rewrite_raw "set -e; is preserved (pipefail goes in front)" \
	'set -e; npm test' \
	$'set -o pipefail\nset -e\nnpm test'

check_rewrite_raw "set -e && chain is preserved" \
	'set -e && npm test' \
	$'set -o pipefail\nset -e && npm test'

check_rewrite_raw "set -e on its own line is preserved" \
	$'set -e\nnpm test' \
	$'set -o pipefail\nset -e\nnpm test'

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
# Only the trailing (rightmost) && / || member is at the end of the command;
# a limiting pipe on an earlier member is deliberate and stays.
check_rewrite "head/tail stripped only in the trailing && member" \
	'a | head -2 && b | tail -3' \
	'a | head -2 && b'
check_rewrite "scrub is tree-wide but head strip is trailing-only" \
	'foo 2>/dev/null | head -3 && bar 2> /dev/null' \
	'foo | head -3 && bar'

check_rewrite "| headache untouched (word boundary)" 'cmd | headache' 'cmd | headache'
check_rewrite "| tailscale untouched (word boundary)" 'cmd | tailscale status' 'cmd | tailscale status'
check_rewrite "| head5 untouched (word boundary)" 'cmd | head5' 'cmd | head5'

# Command/process substitutions are functional capture, not output
# truncation -- never stripped there.
check_rewrite "head inside \$() capture preserved" 'VAR=$(ls | head -1)' 'VAR=$(ls | head -1)'
check_rewrite "head inside plain command substitution preserved" 'echo $(ls | head -1)' 'echo $(ls | head -1)'
check_rewrite "head inside process substitution preserved" 'diff <(ls | head -2) file' 'diff <(ls | head -2) file'

# AST-aware: pipes inside string literals are not pipelines.
check_rewrite "literal-string carrier containing | head untouched" ': "foo | head"' ': "foo | head"'
check_rewrite "single-quoted trailing | head untouched" ": 'try: cmd | head -3'" ": 'try: cmd | head -3'"

# Multi-line commands: head/tail is FINAL-statement-only. A limiting pipe on
# an earlier line is an intentional part of a longer script and is kept; the
# wild "removed too much" incident is exactly this shape.
check_rewrite "head on a non-final line preserved" \
	$'foo | head -3\ncat done' \
	$'foo | head -3\ncat done'
check_rewrite "tail on a non-final line preserved" \
	$'ls -1 "$D" | tail -12\ncat done' \
	$'ls -1 "$D" | tail -12\ncat done'
check_rewrite "stdout redirect on a non-final line preserved" \
	$'make >build.log\ncat build.log' \
	$'make >build.log\ncat build.log'
check_rewrite "tail on final line is trailing" \
	$'foo\nbar | tail -5' \
	$'foo\nbar'

check_rewrite_raw "multi-line mix of all rules (set -e preserved)" \
	$'set -e; ls /x 2>/dev/null | head -3\ngrep -r pat . 2>>/dev/null || true' \
	$'set -o pipefail\nset -e\nls /x | head -3\ngrep -r pat .'

# --- Legacy trailing rules ---

check_rewrite "trailing 2>&1 removed" \
	'ls -la 2>&1' \
	'ls -la'

check_rewrite "trailing || true" 'rm -f foo || true' 'rm -f foo'
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
check_rewrite "2>&1 with || true" 'cmd 2>&1 || true' 'cmd'
check_rewrite "head after 2>&1" 'ls -la 2>&1 | head -20' 'ls -la'
check_rewrite "tail then grep chain" 'cmd | tail -10 | grep foo' 'cmd'
# AST improvement over the old text rule, which could not tell a quoted
# pipe from a real one and skipped this grep.
check_rewrite "trailing grep with quoted pipe arg is stripped" \
	'cmd | grep "foo|bar"' \
	'cmd'

check_rewrite "|| true mid-command untouched" 'cmd || true && cat done' 'cmd || true && cat done'
check_rewrite "head mid-pipeline untouched" 'cmd | head -5 | wc' 'cmd | head -5 | wc'
check_rewrite "tail mid-pipeline untouched" 'cmd | tail -10 | wc' 'cmd | tail -10 | wc'
check_rewrite "grep mid-pipeline untouched" 'cmd | grep foo | wc' 'cmd | grep foo | wc'
# grep is end-of-command anchored (same anchoring as head/tail now).
check_rewrite "grep on a non-final line untouched" $'cmd | grep x\ncat done' $'cmd | grep x\ncat done'
# Whitespace-only differences are not semantic; only pipefail fires.
check_rewrite "whitespace-only difference gets only pipefail" '  ls -la  ' 'ls -la'

# --- Multi-statement scripts: mid-script pipes/redirects/2>&1 all preserved ---

# Semicolon-joined statements come back one per line (shfmt-canonical); every
# non-final limiting pipe, stdout redirect, and 2>&1 survives, as do the
# assignments between them. Only the final statement is trailing.
check_rewrite "mid-script limiting pipes and redirects preserved" \
	'x=1; ls | tail -2; cat f | head -3; du -sh . 2>&1; make >build.log; cat done' \
	$'x=1\nls | tail -2\ncat f | head -3\ndu -sh . 2>&1\nmake >build.log\ncat done'

check_rewrite "final statement still trailing in a multi-statement script" \
	$'x=1\nls | tail -2\ncat f | head -3' \
	$'x=1\nls | tail -2\ncat f'

# --- Sleep cap: every sleep, everywhere, capped at 3 seconds ---

check_rewrite "sleep over-cap integer capped" 'sleep 30' 'sleep 3'
check_rewrite "sleep over-cap float capped" 'sleep 10.5' 'sleep 3'
check_rewrite "sleep under cap kept" 'sleep 2' 'sleep 2'
check_rewrite "sleep boundary 3 kept" 'sleep 3' 'sleep 3'
check_rewrite "sleep 0.5s suffix under cap kept" 'sleep 0.5s' 'sleep 0.5s'
check_rewrite "sleep leading-dot float kept" 'sleep .5' 'sleep .5'
check_rewrite "sleep 30s capped" 'sleep 30s' 'sleep 3'
check_rewrite "sleep minutes capped" 'sleep 1m' 'sleep 3'
check_rewrite "sleep hours capped" 'sleep 2h' 'sleep 3'
check_rewrite "sleep days capped" 'sleep 1d' 'sleep 3'
check_rewrite "sleep multi-arg sum over cap" 'sleep 1m 30' 'sleep 3'
check_rewrite "sleep multi-arg sum under cap kept" 'sleep 1 1' 'sleep 1 1'
check_rewrite "sleep multi-arg float sum just over" 'sleep 1.5 1.6' 'sleep 3'
check_rewrite "sleep infinity capped" 'sleep infinity' 'sleep 3'
check_rewrite "sleep variable arg capped" 'sleep $DELAY' 'sleep 3'
check_rewrite "sleep cmdsubst arg capped" 'sleep "$(get_delay)"' 'sleep 3'
check_rewrite "sleep junk arg capped" 'sleep abc' 'sleep 3'
check_rewrite "sleep scientific notation is junk" 'sleep 1e-3' 'sleep 3'
check_rewrite "sleep zero args capped" 'sleep' 'sleep 3'
check_rewrite "sleep quoted literal under cap kept" 'sleep "2"' 'sleep "2"'
check_rewrite "sleep quoted suffix capped" "sleep '1m'" 'sleep 3'
check_rewrite "sleep prefix assignment preserved" 'FOO=1 sleep 30' 'FOO=1 sleep 3'
check_rewrite "sleep redirect preserved" 'sleep 30 2>>err.log' 'sleep 3 2>>err.log'
check_rewrite "sleep capped inside while-loop retry" \
	'while ! nc -z h 22; do sleep 30; done' \
	'while ! nc -z h 22; do sleep 3; done'
check_rewrite "sleep capped inside command substitution" 'VAR=$(sleep 30)' 'VAR=$(sleep 3)'
check_rewrite "sleep capped inside subshell" '(sleep 45)' '(sleep 3)'
check_rewrite "sleep capped inside function body" 'f() { sleep 60; }' 'f() { sleep 3; }'
check_rewrite "sleep capped in || branch" 'nc -z h 22 || sleep 30' 'nc -z h 22 || sleep 3'
check_rewrite "sleep cap is per command not per script" 'sleep 2 && sleep 2' 'sleep 2 && sleep 2'
check_rewrite "sleep as timeout arg untouched" 'timeout 5 sleep 30' 'timeout 5 sleep 30'
check_rewrite "sleep inside string word untouched" 'grep "sleep 30" app.log' 'grep "sleep 30" app.log'

# --- Echo nag: constant echoes whose stdout reaches the terminal ---

check_rewrite "echo separator pattern nagged" 'echo "=== section ==="' "$NAG"
check_rewrite "echo unquoted narration nagged" 'echo starting build now' "$NAG"
check_rewrite "echo -n flag dropped by nag" 'echo -n "x"' "$NAG"
check_rewrite "bare echo nagged" 'echo' "$NAG"
check_rewrite "echo empty string nagged" 'echo ""' "$NAG"
check_rewrite "echo ansi-c quoting is constant" "echo \$'a\\nb'" "$NAG"
check_rewrite "echo exact replacement text" \
	$'echo "has spaces and \'quotes\'"' \
	"$NAG"
check_rewrite "echo nagged in && member" 'echo done && rm -f x' "$NAG && rm -f x"
check_rewrite "echo nagged as final pipe stage" 'x | echo foo' "x | $NAG"
check_rewrite "echo nagged in if condition" 'if echo checking; then ls; fi' "if $NAG; then ls; fi"
check_rewrite "echo nagged in for-loop body" \
	'for i in a b; do echo checking; done' \
	"for i in a b; do $NAG; done"
check_rewrite "echo nagged in until-loop body" \
	'until ready; do echo waiting; done' \
	"until ready; do $NAG; done"
check_rewrite "echo nagged in elif/else chain" \
	'if a; then echo x; elif b; then echo y; else echo z; fi' \
	"if a; then $NAG; elif b; then $NAG; else $NAG; fi"
# from-json puts case items on separate lines once the replaced words carry
# no position info -- layout artifact only, content identical.
check_rewrite "echo nagged in case items" \
	'case $x in a) echo one ;; *) echo other ;; esac' \
	"case \$x in a) $NAG ;;"$'\n'"*) $NAG ;;"$'\n'"esac"
check_rewrite "echo nagged under time" 'time echo hi' "time $NAG"
check_rewrite "negated echo keeps negation" '! echo ok' "! $NAG"
check_rewrite "echo nagged in nested while+if" \
	'while x; do if y; then echo deep; fi; done' \
	"while x; do if y; then $NAG; fi; done"
check_rewrite "echo nagged in subshell" '(echo hi)' "($NAG)"
check_rewrite "echo stderr redirect kept by nag" 'echo warn 2>>err.log' "$NAG 2>>err.log"
check_rewrite "head-strip then echo nag compose" 'echo foo | head -1' "$NAG"

# Not narration: expansions, captures, redirected or pipe-feeding stdout.
check_rewrite "echo variable untouched" 'echo "$VAR"' 'echo "$VAR"'
check_rewrite "echo cmdsubst untouched" 'echo $(date)' 'echo $(date)'
check_rewrite "echo arithmetic untouched" 'echo $((1 + 2))' 'echo $((1 + 2))'
check_rewrite "echo glob untouched" 'echo *.txt' 'echo *.txt'
check_rewrite "echo tilde untouched" 'echo ~' 'echo ~'
check_rewrite "echo assignment capture untouched" 'X=$(echo abc)' 'X=$(echo abc)'
check_rewrite "echo pipe feed untouched" 'echo foo | cat' 'echo foo | cat'
check_rewrite "echo data into jq untouched" \
	"echo '{\"x\":1}' | jq .x" \
	"echo '{\"x\":1}' | jq .x"
check_rewrite "echo stderr dup >&2 untouched" 'echo foo >&2' 'echo foo >&2'
check_rewrite "echo stdout file redirect gets tee only" 'echo foo > file' 'echo foo | tee file'
check_rewrite "redirected block group stays data" '{ echo a; } > f' '{ echo a; } | tee f'
check_rewrite "redirected for-loop stays data" \
	'for i in a; do echo x; done > log' \
	'for i in a; do echo x; done | tee log'
check_rewrite "echo in function body untouched" 'f() { echo hi; }' 'f() { echo hi; }'
check_rewrite "echo in coproc untouched" 'coproc echo hi' 'coproc echo hi'
check_rewrite "echo in process substitution untouched" \
	'foo | tee >(echo inside)' \
	'foo | tee >(echo inside)'

# --- printf nag: same rule as echo, same terminal-visibility guard ---
# printf is nagged identically to echo; Args[0] (the command word) is
# preserved, so a nagged printf renders with the printf command word.

check_rewrite "printf separator pattern nagged" 'printf "=== section ==="' "$NAG_PRINTF"
check_rewrite "printf unquoted narration nagged" 'printf starting build now' "$NAG_PRINTF"
check_rewrite "printf nagged in && member" 'printf done && rm -f x' "$NAG_PRINTF && rm -f x"
check_rewrite "printf nagged as final pipe stage" 'x | printf foo' "x | $NAG_PRINTF"
check_rewrite "printf stderr redirect kept by nag" 'printf warn 2>>err.log' "$NAG_PRINTF 2>>err.log"

# Not narration: expansions, captures, redirected or pipe-feeding stdout --
# the identical carve-outs to echo, since printf rides the same rule.
check_rewrite "printf variable untouched" 'printf "$VAR"' 'printf "$VAR"'
check_rewrite "printf pipe feed untouched" 'printf foo | cat' 'printf foo | cat'
check_rewrite "printf stdout file redirect gets tee only" 'printf foo > out.txt' 'printf foo | tee out.txt'
check_rewrite "printf cmdsubst untouched" 'printf "$(date)"' 'printf "$(date)"'
check_rewrite "printf in function body untouched" 'f() { printf hi; }' 'f() { printf hi; }'
check_rewrite "printf in coproc untouched" 'coproc printf hi' 'coproc printf hi'
check_rewrite "printf %s payload into pipe untouched (heredoc-deny form)" "printf '%s' payload | cat" "printf '%s' payload | cat"

# Idempotency: the rewritten output is a fixpoint (turn 2 does not rewrite).
check_noop "nag output is a fixpoint" "${PFX}${NAG}"
check_noop "sleep cap output is a fixpoint" "${PFX}sleep 3"

# Combined multi-statement script: both rules fire, every statement survives.
check_rewrite "combined script preserves all statements" \
	$'echo "=== deploy ==="\nscp app host:/srv/ 2>/dev/null\nwhile ! ssh host ok; do sleep 30; done\nout=$(echo probe)\necho done | tee -a log\necho finished' \
	"$NAG"$'\nscp app host:/srv/\nwhile ! ssh host ok; do sleep 3; done\nout=$(echo probe)\necho done | tee -a log\n'"$NAG"

run_hook "$(payload_bash $'echo "=== deploy ==="\nscp app host:/srv/ 2>/dev/null\nwhile ! ssh host ok; do sleep 30; done\nout=$(echo probe)\necho done | tee -a log\necho finished')"
NSTMT=$(printf '%s' "$OUT" | jq -r '.hookSpecificOutput.updatedInput.command' | shfmt --to-json | jq '.Stmts | length')
if [ "$NSTMT" = "7" ]; then
	ok "combined script statement count is 6 + pipefail"
else
	bad "combined script statement count is 6 + pipefail" "got $NSTMT statements"
fi

# All three new behaviors in one script, with a statement-count assertion:
# echo_nag on the narration, sleep_cap on the wait, head_tail on the final
# (and only the final) statement.
check_rewrite "echo_nag + sleep_cap + final head_tail compose" \
	$'echo start\nsleep 10\nls | tail -5' \
	"$NAG"$'\nsleep 3\nls'

run_hook "$(payload_bash $'echo start\nsleep 10\nls | tail -5')"
NSTMT=$(printf '%s' "$OUT" | jq -r '.hookSpecificOutput.updatedInput.command' | shfmt --to-json | jq '.Stmts | length')
if [ "$NSTMT" = "4" ]; then
	ok "composed rewrite statement count is 3 + pipefail"
else
	bad "composed rewrite statement count is 3 + pipefail" "got $NSTMT statements"
fi

# --- Regression: the wild "removed statements after | tail -12" incident ---
# Byte-exact input from the incident (the stale regex-era hook swallowed
# every statement after the mid-script `| tail -12`). The AST hook must keep
# ALL top-level statements: the mid-script tail and its 2>&1 stay, the three
# constant narration echoes are nagged, and both splat-compare invocations
# survive verbatim.

REPRO_IN='cd /Users/mhaynie/repos/splat/splat-vulkan
PRE=/private/tmp/claude-501/-Users-mhaynie-repos-splat/10878a52-8108-4974-ae5c-3ecf2bb62d3f/scratchpad/premerge-wt/splat-vulkan/build/test-out/t5b
POST=build/test-out/t5b
echo "=== files present ==="; ls -1 "$PRE" "$POST" 2>&1 | tail -12
echo "=== REF: pre-merge vs post-merge ==="; ./build/splat-compare "$PRE/odd-size-ref.png" "$POST/odd-size-ref.png" --within 2 --pct 99 --mean 0.5
echo "=== FROXEL: pre-merge vs post-merge ==="; ./build/splat-compare "$PRE/odd-size-froxel.png" "$POST/odd-size-froxel.png" --within 2 --pct 99 --mean 0.5'

REPRO_WANT='cd /Users/mhaynie/repos/splat/splat-vulkan
PRE=/private/tmp/claude-501/-Users-mhaynie-repos-splat/10878a52-8108-4974-ae5c-3ecf2bb62d3f/scratchpad/premerge-wt/splat-vulkan/build/test-out/t5b
POST=build/test-out/t5b
'"$NAG"'
ls -1 "$PRE" "$POST" 2>&1 | tail -12
'"$NAG"'
./build/splat-compare "$PRE/odd-size-ref.png" "$POST/odd-size-ref.png" --within 2 --pct 99 --mean 0.5
'"$NAG"'
./build/splat-compare "$PRE/odd-size-froxel.png" "$POST/odd-size-froxel.png" --within 2 --pct 99 --mean 0.5'

check_rewrite "splat repro keeps every statement (tail -12 and 2>&1 intact)" \
	"$REPRO_IN" \
	"$REPRO_WANT"

run_hook "$(payload_bash "$REPRO_IN")"
RCMD=$(printf '%s' "$OUT" | jq -r '.hookSpecificOutput.updatedInput.command')
RSTMT=$(printf '%s' "$RCMD" | shfmt --to-json | jq '.Stmts | length')
if [ "$RSTMT" = "10" ]; then
	ok "splat repro statement count is 9 + pipefail"
else
	bad "splat repro statement count is 9 + pipefail" "got $RSTMT statements"
fi
if [ "$(printf '%s' "$RCMD" | grep -cF "$NAG")" = "3" ] &&
	[ "$(printf '%s' "$RCMD" | grep -cF './build/splat-compare "$PRE')" = "2" ] &&
	printf '%s' "$RCMD" | grep -qF '2>&1 | tail -12'; then
	ok "splat repro markers: 3 nags, 2 splat-compares, tail/2>&1 kept"
else
	bad "splat repro markers: 3 nags, 2 splat-compares, tail/2>&1 kept" "cmd=$(printf '%q' "$RCMD")"
fi

# Execution equivalence on a relocated twin (same shape, stub paths under
# mktemp so it runs unprivileged): the rewritten script must execute ALL
# statements -- 3 nag lines where the 3 headers were, the same tail-limited
# ls output, and both splat-compare invocations, line-for-line.
STUB=$(mktemp -d)
mkdir -p "$STUB/repo/build/test-out/t5b" "$STUB/pre/t5b"
printf '#!/bin/sh\nprintf "splat-compare ran: %%s\\n" "$*"\n' >"$STUB/repo/build/splat-compare"
chmod +x "$STUB/repo/build/splat-compare"
touch "$STUB/pre/t5b/odd-size-ref.png" "$STUB/pre/t5b/odd-size-froxel.png" \
	"$STUB/repo/build/test-out/t5b/odd-size-ref.png" "$STUB/repo/build/test-out/t5b/odd-size-froxel.png"
TWIN_IN='cd '"$STUB"'/repo
PRE='"$STUB"'/pre/t5b
POST=build/test-out/t5b
echo "=== files present ==="; ls -1 "$PRE" "$POST" 2>&1 | tail -12
echo "=== REF: pre-merge vs post-merge ==="; ./build/splat-compare "$PRE/odd-size-ref.png" "$POST/odd-size-ref.png" --within 2 --pct 99 --mean 0.5
echo "=== FROXEL: pre-merge vs post-merge ==="; ./build/splat-compare "$PRE/odd-size-froxel.png" "$POST/odd-size-froxel.png" --within 2 --pct 99 --mean 0.5'
run_hook "$(payload_bash "$TWIN_IN")"
TWIN_CMD=$(printf '%s' "$OUT" | jq -r '.hookSpecificOutput.updatedInput.command')
set +e
ORIG_OUT=$(bash -c "$TWIN_IN" 2>&1)
TWIN_OUT=$(bash -c "$TWIN_CMD" 2>&1)
set -e
orig_lines=$(printf '%s\n' "$ORIG_OUT" | wc -l)
twin_lines=$(printf '%s\n' "$TWIN_OUT" | wc -l)
twin_nags=$(printf '%s\n' "$TWIN_OUT" | grep -cF 'system message: do not use echo/printf to communicate with the user')
twin_splats=$(printf '%s\n' "$TWIN_OUT" | grep -cF 'splat-compare ran:')
if [ "$twin_lines" = "$orig_lines" ] && [ "$twin_nags" = "3" ] && [ "$twin_splats" = "2" ]; then
	ok "relocated repro executes all statements (3 nags + ls + 2 splat-compares)"
else
	bad "relocated repro executes all statements" "orig_lines=$orig_lines twin_lines=$twin_lines nags=$twin_nags splats=$twin_splats out=$(printf '%q' "$TWIN_OUT")"
fi
rm -rf "$STUB"

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

# No pipefail injection into something we cannot parse.
check_noop "unparseable bash fails open (no injection)" 'if true; then'

# The tool-missing hooks exit before reading stdin, so feed the payload from
# a file: piping would race the writer against the early exit and surface as
# a spurious SIGPIPE (status 141) under pipefail on fast machines.
PAYLOAD_FILE=$(mktemp)
payload_bash 'ls 2>/dev/null' >"$PAYLOAD_FILE"

FAKEBIN=$(mktemp -d)
ln -s "$(command -v bash)" "$FAKEBIN/bash"
set +e
NOJQ_OUT=$(env PATH="$FAKEBIN" "$HOOK" <"$PAYLOAD_FILE")
NOJQ_STATUS=$?
set -e
rm -rf "$FAKEBIN"
if [ "$NOJQ_STATUS" -eq 0 ] && [ -z "$NOJQ_OUT" ]; then
	ok "missing jq fails open"
else
	bad "missing jq fails open" "status=$NOJQ_STATUS out=$(printf '%q' "$NOJQ_OUT")"
fi

FAKEBIN=$(mktemp -d)
ln -s "$(command -v bash)" "$FAKEBIN/bash"
ln -s "$(command -v jq)" "$FAKEBIN/jq"
set +e
NOSHFMT_OUT=$(env PATH="$FAKEBIN" "$HOOK" <"$PAYLOAD_FILE")
NOSHFMT_STATUS=$?
set -e
rm -rf "$FAKEBIN"
rm -f "$PAYLOAD_FILE"
if [ "$NOSHFMT_STATUS" -eq 0 ] && [ -z "$NOSHFMT_OUT" ]; then
	ok "missing shfmt fails open"
else
	bad "missing shfmt fails open" "status=$NOSHFMT_STATUS out=$(printf '%q' "$NOSHFMT_OUT")"
fi

# --- No-statement-loss guard: a transform that drops a statement fails open ---
# Sabotage: a copy of the hook whose transform claims a change but deletes
# the last top-level statement. The post-regeneration re-parse guard must
# refuse to emit the truncated rewrite (exit 0, no output, silent) and leave
# a GUARD line in the debug log. The real transform can never trip this (it
# only maps over, edits within, or prepends to .Stmts) -- which the rest of
# this suite proves by never firing the guard on a legitimate rewrite.
GUARDDIR=$(mktemp -d)
cp "$HOOK" "$GUARDDIR/hook.sh"
printf '%s\n' '{deny: false, changed: true, rules: "evil", ast: (.Stmts |= .[0:-1])}' >"$GUARDDIR/transform.jq"
GUARDLOG=$GUARDDIR/guard.log
set +e
GOUT=$(printf '%s' "$(payload_bash $'ls one\nls two')" | CLEANUP_BASH_CMDS_LOG="$GUARDLOG" "$GUARDDIR/hook.sh")
GSTATUS=$?
set -e
if [ "$GSTATUS" -eq 0 ] && [ -z "$GOUT" ]; then
	ok "statement-eating transform fails open (no rewrite emitted)"
else
	bad "statement-eating transform fails open (no rewrite emitted)" "status=$GSTATUS out=$(printf '%q' "$GOUT")"
fi
if [ -f "$GUARDLOG" ] && grep -q '^GUARD	original="ls one\\nls two"	cleaned="ls one"	reason="stmt-count"$' "$GUARDLOG"; then
	ok "guard event logged with GUARD tag and stmt-count reason"
else
	bad "guard event logged with GUARD tag and stmt-count reason" "log=$(cat "$GUARDLOG" || printf 'missing')"
fi
rm -rf "$GUARDDIR"

# --- Output JSON shape (verified schema, claude-code 2.1.201) ---
# Same silent shape for every rule combination.

run_hook "$(payload_bash 'ls /nope 2>/dev/null | head -3 > out || true')"
if [ "$STATUS" -eq 0 ] && printf '%s' "$OUT" | jq -e '
	(keys == ["hookSpecificOutput", "suppressOutput"]) and
	(.suppressOutput == true) and
	(.hookSpecificOutput | keys == ["hookEventName", "updatedInput"]) and
	(.hookSpecificOutput.hookEventName == "PreToolUse")
' >/dev/null; then
	ok "multi-rule rewrite output is exactly {suppressOutput, updatedInput}"
else
	bad "multi-rule rewrite output is exactly {suppressOutput, updatedInput}" "out=$(printf '%q' "$OUT")"
fi

run_hook "$(payload_bash 'git status')"
if [ "$STATUS" -eq 0 ] && printf '%s' "$OUT" | jq -e '
	(keys == ["hookSpecificOutput", "suppressOutput"]) and
	(.suppressOutput == true) and
	(.hookSpecificOutput | keys == ["hookEventName", "updatedInput"])
' >/dev/null; then
	ok "pipefail-only rewrite output is exactly {suppressOutput, updatedInput}"
else
	bad "pipefail-only rewrite output is exactly {suppressOutput, updatedInput}" "out=$(printf '%q' "$OUT")"
fi

run_hook "$(payload_bash 'ls /nope 2>/dev/null')"
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
		command: "set -o pipefail\nnpm install",
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

# --- Rewrite logging (CLEANUP_BASH_CMDS_LOG): the debug channel ---

LOGDIR=$(mktemp -d)
LOGFILE=$LOGDIR/hook.log

OUT=$(printf '%s' "$(payload_bash 'ls | grep foo')" | CLEANUP_BASH_CMDS_LOG="$LOGFILE" "$HOOK")
if [ -f "$LOGFILE" ] && grep -qF 'original="ls | grep foo"' "$LOGFILE" &&
	grep -qF 'cleaned="set -o pipefail\nls"' "$LOGFILE" &&
	grep -qF 'rules="grep,pipefail"' "$LOGFILE"; then
	ok "rewrite logged with its rule tags"
else
	bad "rewrite logged with its rule tags" "log=$(cat "$LOGFILE" || printf 'missing')"
fi

OUT=$(printf '%s' "$(payload_bash 'git status')" | CLEANUP_BASH_CMDS_LOG="$LOGFILE" "$HOOK")
if grep -qE 'original="git status".*rules="pipefail"' "$LOGFILE"; then
	ok "pipefail-only rewrite logged too"
else
	bad "pipefail-only rewrite logged too" "log=$(cat "$LOGFILE" || printf 'missing')"
fi

if [ "$(grep -c '^REWRITE	' "$LOGFILE")" -eq 2 ]; then
	ok "log appends across invocations"
else
	bad "log appends across invocations" "log=$(cat "$LOGFILE" || printf 'missing')"
fi

rm -f "$LOGFILE"
OUT=$(printf '%s' "$(payload_bash 'set -o pipefail; ls')" | CLEANUP_BASH_CMDS_LOG="$LOGFILE" "$HOOK")
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

OUT=$(printf '%s' "$(payload_bash $'cat <<EOF\nhi\nEOF')" | CLEANUP_BASH_CMDS_LOG="$LOGFILE" "$HOOK")
if [ -f "$LOGFILE" ] && grep -q '^DENY	original="cat <<EOF\\nhi\\nEOF"	reason="heredoc"$' "$LOGFILE"; then
	ok "deny is logged with action DENY"
else
	bad "deny is logged with action DENY" "log=$(cat "$LOGFILE" || printf 'missing')"
fi

rm -f "$LOGFILE"
OUT=$(printf '%s' "$(payload_bash 'sleep 30; echo hi')" | CLEANUP_BASH_CMDS_LOG="$LOGFILE" "$HOOK")
if grep -qF 'rules="sleep_cap,echo_nag,pipefail"' "$LOGFILE"; then
	ok "sleep_cap and echo_nag rule tags logged"
else
	bad "sleep_cap and echo_nag rule tags logged" "log=$(cat "$LOGFILE" || printf 'missing')"
fi

rm -rf "$LOGDIR"

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
