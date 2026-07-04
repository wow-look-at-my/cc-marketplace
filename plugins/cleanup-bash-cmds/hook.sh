#!/usr/bin/env bash
# PreToolUse hook: cleans up Bash tool commands before they run.
#
# The command is PARSED, not pattern-matched: shfmt --to-json produces the
# syntax tree, transform.jq rewrites it (scrub stderr-to-/dev/null redirects
# everywhere; kill trailing | head / | tail stages; legacy noise rules), and
# shfmt --from-json regenerates the command. Only a semantic AST change
# triggers a rewrite, so string literals that merely contain "2>/dev/null"
# or "| head" are never mangled.
#
# Operator tokens in shfmt's typed JSON are version-dependent numbers, so
# they are probed at runtime from the same shfmt binary (see $probe below).
#
# If the command needs rewriting, emits hookSpecificOutput JSON carrying
# updatedInput on stdout and exits 0. The output deliberately carries NO
# permissionDecision: the normal permission flow continues and evaluates the
# rewritten command (verified against @anthropic-ai/claude-code 2.1.201:
# cli.js:199270 schema, cli.js:393479 hookUpdatedInput event, cli.js:408198
# input replacement).
#
# Fail-open policy: if jq or shfmt is missing, stdin is not valid hook JSON,
# the tool is not Bash, the command is missing/empty, or the command does not
# parse as bash, exit 0 with no output.
#
# This script never suppresses stderr with /dev/null itself; that would be
# hypocritical. Tool stderr is captured into the substitution result and
# discarded only when the failing exit code takes the fail-open path.

set -euo pipefail

command -v jq >/dev/null || exit 0
command -v shfmt >/dev/null || exit 0

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

input=$(cat)
[ -n "$input" ] || exit 0

# Single fail-open extraction: empty result unless tool_name is Bash and a
# non-empty command is present.
cmd=$(printf '%s' "$input" | jq -r 'if (.tool_name? == "Bash") then (.tool_input.command? // "") else "" end' 2>&1) || exit 0
[ -n "$cmd" ] || exit 0

# Probe this shfmt build's operator numbers with a fixed script whose ops sit
# at known paths. The numbers differ between shfmt versions ("|" is 12 in
# v3.8.0 but 13 in v3.13.1), so they can never be hardcoded.
probe=$(printf '%s' $': 2>/dev/null\n: 2>>/dev/null\n: 2>&1\n: && :\n: || :\n: | :\n: |& :' | shfmt --to-json 2>&1) || exit 0
ops=$(printf '%s' "$probe" | jq -c '{
	gt: .Stmts[0].Redirs[0].Op,
	app: .Stmts[1].Redirs[0].Op,
	dup: .Stmts[2].Redirs[0].Op,
	and: .Stmts[3].Cmd.Op,
	or: .Stmts[4].Cmd.Op,
	pipe: .Stmts[5].Cmd.Op,
	pipeall: .Stmts[6].Cmd.Op
}' 2>&1) || exit 0
printf '%s' "$ops" | jq -e 'all(.[]; type == "number")' >/dev/null 2>&1 || exit 0

# Parse the command; anything that is not valid bash fails open.
ast=$(printf '%s' "$cmd" | shfmt --to-json 2>&1) || exit 0

result=$(printf '%s' "$ast" | jq -c --argjson ops "$ops" -f "$SCRIPT_DIR/transform.jq" 2>&1) || exit 0

changed=$(printf '%s' "$result" | jq -r '.changed' 2>&1) || exit 0
[ "$changed" = "true" ] || exit 0

cleaned=$(printf '%s' "$result" | jq -c '.ast' | shfmt --from-json 2>&1) || exit 0
# from-json terminates the output with a newline; command substitution
# strips it. Never emit an empty command.
[ -n "$cleaned" ] || exit 0
[ "$cleaned" = "$cmd" ] && exit 0

# Best-effort rewrite log, same env var and line format as the original Go
# implementation (Go %q approximated by escaping backslash, quote, newline,
# and tab). Failures never break the hook.
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
