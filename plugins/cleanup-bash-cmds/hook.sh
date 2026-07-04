#!/usr/bin/env bash
# PreToolUse hook: cleans up Bash tool commands before they run.
#
# Ported 1:1 from the previous Go implementation (hook.go), plus one new rule:
# ALL stderr-to-/dev/null redirections are scrubbed from anywhere in the
# command, because silencing stderr hides real errors.
#
# Reads the hook event JSON on stdin. If the command needs rewriting, emits
# hookSpecificOutput JSON carrying updatedInput on stdout and exits 0. The
# output deliberately carries NO permissionDecision: the normal permission
# flow continues and evaluates the rewritten command (verified against
# @anthropic-ai/claude-code 2.1.201: cli.js:199270 schema, cli.js:393479
# hookUpdatedInput event, cli.js:408198 input replacement).
#
# Fail-open policy: if jq is missing, stdin is not valid hook JSON, the tool
# is not Bash, or the command is missing/empty, exit 0 with no output.
#
# This script never suppresses stderr with /dev/null itself; that would be
# hypocritical.

set -euo pipefail

command -v jq >/dev/null || exit 0

input=$(cat)
[ -n "$input" ] || exit 0

# Single fail-open extraction: empty result unless tool_name is Bash and a
# non-empty command is present. On jq parse errors the captured text is
# discarded and the nonzero exit takes the fail-open path.
cmd=$(printf '%s' "$input" | jq -r 'if (.tool_name? == "Bash") then (.tool_input.command? // "") else "" end' 2>&1) || exit 0
[ -n "$cmd" ] || exit 0

# Equivalent of Go strings.TrimSpace (leading/trailing [[:space:]]).
trim_space() {
	local s=$1
	local re='^[[:space:]]*(.*[^[:space:]])[[:space:]]*$'
	if [[ $s =~ $re ]]; then
		printf '%s' "${BASH_REMATCH[1]}"
	fi
}

# Applies the cleanup rules until the command stabilizes (max 10 passes, same
# as the Go implementation). Prints the cleaned command on stdout.
clean_command() {
	local cmd=$1 prev i
	# Ported from hook.go (Go regexp -> POSIX ERE; \s -> [[:space:]], and
	# literal pipes as [|] for portability):
	local re_set_e_semi='^set[[:space:]]+-e[[:space:]]*;[[:space:]]*'
	local re_set_e_and='^set[[:space:]]+-e[[:space:]]*&&[[:space:]]*'
	local re_or_true='[[:space:]]*[|][|][[:space:]]*true[[:space:]]*$'
	local re_stderr_merge='[[:space:]]+2>&1[[:space:]]*$'
	local re_head='[[:space:]]*[|][[:space:]]*head([[:space:]]+[^[:space:]|]+)*[[:space:]]*$'
	local re_tail='[[:space:]]*[|][[:space:]]*tail([[:space:]]+[^[:space:]|]+)*[[:space:]]*$'
	local re_grep='[[:space:]]*[|][[:space:]]*grep([[:space:]]+[^[:space:]|]+)*[[:space:]]*$'
	# New rule: scrub EVERY stderr-to-/dev/null redirection, anywhere in the
	# command (not just trailing). Covered forms: 2>/dev/null, 2> /dev/null,
	# 2>>/dev/null, 2>> /dev/null, and the quoted targets 2>'/dev/null' and
	# 2>"/dev/null". The (^|.*[^0-9]) prefix requires start-of-string or a
	# non-digit before the 2, so multi-digit fds (12>/dev/null) stay intact.
	# The trailing ([^A-Za-z0-9._/-].*)? guard requires end-of-string or a
	# non-path character after the target, so distinct paths such as
	# /dev/null2 or /dev/null.log stay intact. Occurrences inside quoted
	# strings and heredocs ARE scrubbed -- blunt by design.
	local re_devnull='(^|.*[^0-9])2>>?[[:space:]]*("/dev/null"|'\''/dev/null'\''|/dev/null)([^A-Za-z0-9._/-].*)?$'

	for ((i = 0; i < 10; i++)); do
		prev=$cmd
		cmd=$(trim_space "$cmd")
		if [[ $cmd =~ $re_set_e_semi ]]; then cmd=${cmd#"${BASH_REMATCH[0]}"}; fi
		if [[ $cmd =~ $re_set_e_and ]]; then cmd=${cmd#"${BASH_REMATCH[0]}"}; fi
		if [[ $cmd =~ $re_or_true ]]; then cmd=${cmd%"${BASH_REMATCH[0]}"}; fi
		if [[ $cmd =~ $re_stderr_merge ]]; then cmd=${cmd%"${BASH_REMATCH[0]}"}; fi
		# Greedy prefix makes each pass strip the rightmost occurrence; the
		# string strictly shrinks, so this always terminates.
		while [[ $cmd =~ $re_devnull ]]; do
			cmd="${BASH_REMATCH[1]}${BASH_REMATCH[3]:-}"
		done
		if [[ $cmd =~ $re_head ]]; then cmd=${cmd%"${BASH_REMATCH[0]}"}; fi
		if [[ $cmd =~ $re_tail ]]; then cmd=${cmd%"${BASH_REMATCH[0]}"}; fi
		if [[ $cmd =~ $re_grep ]]; then cmd=${cmd%"${BASH_REMATCH[0]}"}; fi
		cmd=$(trim_space "$cmd")
		if [ "$cmd" = "$prev" ]; then break; fi
	done
	printf '%s' "$cmd"
}

# Best-effort rewrite log, same env var and line format as the Go version
# (Go %q approximated by escaping backslash, quote, newline, and tab).
# Failures never break the hook.
log_rewrite() {
	local path=${CLEANUP_BASH_CMDS_LOG:-}
	[ -n "$path" ] || return 0
	local o=$1 c=$2
	o=${o//\\/\\\\}
	o=${o//\"/\\\"}
	o=${o//$'\n'/\\n}
	o=${o//$'\t'/\\t}
	c=${c//\\/\\\\}
	c=${c//\"/\\\"}
	c=${c//$'\n'/\\n}
	c=${c//$'\t'/\\t}
	printf 'REWRITE\toriginal="%s"\tcleaned="%s"\n' "$o" "$c" >>"$path" || true
}

cleaned=$(clean_command "$cmd")
if [ "$cleaned" = "$cmd" ]; then
	exit 0
fi

log_rewrite "$cmd" "$cleaned"

# updatedInput replaces tool_input wholesale, so echo back the ORIGINAL
# tool_input with only .command changed (preserves timeout, description, and
# any other fields). No permissionDecision: the normal permission flow keeps
# running against the rewritten command. systemMessage informs the user;
# additionalContext tells the model what actually ran.
printf '%s' "$input" | jq -c --arg cmd "$cleaned" '{
	systemMessage: "cleanup-bash-cmds: rewrote the Bash command (removed stderr suppression and/or noise patterns); the permission system evaluates the rewritten command.",
	hookSpecificOutput: {
		hookEventName: "PreToolUse",
		updatedInput: (.tool_input | .command = $cmd),
		additionalContext: ("cleanup-bash-cmds rewrote the Bash command before execution. Executed command: " + $cmd)
	}
}' || exit 0
