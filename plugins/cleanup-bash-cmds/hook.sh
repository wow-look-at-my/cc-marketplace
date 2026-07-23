#!/usr/bin/env bash
# PreToolUse hook: cleans up Bash tool commands before they run.
#
# The command is PARSED, not pattern-matched: shfmt --to-json produces the
# syntax tree, transform.jq inspects and rewrites it, and shfmt --from-json
# regenerates the command. Commands containing a heredoc (<< or <<-,
# anywhere in the tree), invoking perl (its effective command name matching
# ^perl[0-9.]*$), or reading a file with cat/head/tail (a static file
# operand that is not a /proc, /sys, or /dev pseudo-file -- use the Read
# tool) are DENIED outright. Otherwise the tree is rewritten:
# scrub stderr-to-/dev/null redirects everywhere; on the FINAL top-level
# statement only, kill trailing | head / | tail stages, strip trailing
# | grep, trailing || true, and trailing 2>&1, and rewrite a trailing stdout
# file redirect into | tee (mid-script limiting pipes and redirects are
# deliberate and preserved); cap every sleep at 3 seconds; remove constant
# terminal-bound echo/printf narration (rewriting it to the no-op `:`); and
# ensure `set -o pipefail` is in effect. Only a semantic AST change triggers a
# rewrite, so string literals
# that merely contain "2>/dev/null" or "| head" are never mangled.
# Strictness settings the user wrote (set -e etc.) are never removed. After
# regeneration the cleaned command is re-parsed and the rewrite is dropped
# entirely (fail open) if it somehow contains fewer top-level statements
# than the original -- a belt-and-braces guard against ever executing a
# truncated command.
#
# The hook is fully SILENT by design: rewrites emit only updatedInput (with
# suppressOutput), never a systemMessage or additionalContext -- a visible
# hook message just gives the model something to blame for its own command
# mistakes. The only observable trace of a rewrite is the executed command
# itself; CLEANUP_BASH_CMDS_LOG is the debug channel. The heredoc deny keeps
# its permissionDecisionReason (without it the model would retry heredocs
# forever) but carries no systemMessage either.
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
# v3.8.0 but 13 in v3.13.1), so they can never be hardcoded. The last two
# statements probe the heredoc operators (<< and <<-).
probe=$(printf '%s' $': 2>/dev/null\n: 2>>/dev/null\n: 2>&1\n: && :\n: || :\n: | :\n: |& :\n: <<CBC_A\nCBC_A\n: <<-CBC_B\nCBC_B' | shfmt --to-json 2>&1) || exit 0
ops=$(printf '%s' "$probe" | jq -c '{
	gt: .Stmts[0].Redirs[0].Op,
	app: .Stmts[1].Redirs[0].Op,
	dup: .Stmts[2].Redirs[0].Op,
	and: .Stmts[3].Cmd.Op,
	or: .Stmts[4].Cmd.Op,
	pipe: .Stmts[5].Cmd.Op,
	pipeall: .Stmts[6].Cmd.Op,
	hdoc: .Stmts[7].Redirs[0].Op,
	dashhdoc: .Stmts[8].Redirs[0].Op
}' 2>&1) || exit 0
printf '%s' "$ops" | jq -e 'all(.[]; type == "number")' >/dev/null 2>&1 || exit 0

# Parse the command; anything that is not valid bash fails open.
ast=$(printf '%s' "$cmd" | shfmt --to-json 2>&1) || exit 0

result=$(printf '%s' "$ast" | jq -c --argjson ops "$ops" -f "$SCRIPT_DIR/transform.jq" 2>&1) || exit 0

# Best-effort log (CLEANUP_BASH_CMDS_LOG), same env var and line format
# family as the original Go implementation (Go %q approximated by escaping
# backslash, quote, newline, and tab). Failures never break the hook.
log_escape() {
	local s=$1
	s=${s//\\/\\\\}
	s=${s//\"/\\\"}
	s=${s//$'\n'/\\n}
	s=${s//$'\t'/\\t}
	printf '%s' "$s"
}

# Every rewrite is logged with the rule names that fired -- since the hook
# is silent toward user and model, the log file is the debug channel.
log_rewrite() {
	local path=${CLEANUP_BASH_CMDS_LOG:-}
	[ -n "$path" ] || return 0
	printf 'REWRITE\toriginal="%s"\tcleaned="%s"\trules="%s"\n' "$(log_escape "$1")" "$(log_escape "$2")" "$(log_escape "$3")" >>"$path" || true
}

log_deny() {
	local path=${CLEANUP_BASH_CMDS_LOG:-}
	[ -n "$path" ] || return 0
	printf 'DENY\toriginal="%s"\treason="%s"\n' "$(log_escape "$1")" "$(log_escape "$2")" >>"$path" || true
}

log_guard() {
	local path=${CLEANUP_BASH_CMDS_LOG:-}
	[ -n "$path" ] || return 0
	printf 'GUARD\toriginal="%s"\tcleaned="%s"\treason="stmt-count"\n' "$(log_escape "$1")" "$(log_escape "$2")" >>"$path" || true
}

# Heredocs are banned outright: deny beats rewrite. Herestrings (<<<) are
# fine and never denied. Detection is AST-based (Redirect nodes only), so a
# string containing "<<EOF" or an arithmetic bit shift ($((x << 2)), which
# shares the << token number) is not affected.
deny=$(printf '%s' "$result" | jq -r '.deny' 2>&1) || exit 0
if [ "$deny" = "true" ]; then
	# The transform tags the reason via .rules (heredoc | perl | file_read);
	# pick the matching message. Anything else falls back to the heredoc text.
	deny_rule=$(printf '%s' "$result" | jq -r '.rules' 2>&1) || exit 0
	log_deny "$cmd" "$deny_rule"
	case "$deny_rule" in
	perl)
		reason="perl is banned in this environment."
		;;
	file_read)
		reason="Reading files with cat/head/tail is banned in this environment. Use the Read tool instead. Only /proc, /sys, and /dev pseudo-files are exempt."
		;;
	*)
		reason="Heredocs are banned in this environment. Write file content with the Write/Edit tools; for command stdin use printf '%s' ... | cmd or a temp file."
		;;
	esac
	jq -cn --arg reason "$reason" '{
		hookSpecificOutput: {
			hookEventName: "PreToolUse",
			permissionDecision: "deny",
			permissionDecisionReason: $reason
		}
	}' || exit 0
	exit 0
fi

changed=$(printf '%s' "$result" | jq -r '.changed' 2>&1) || exit 0
[ "$changed" = "true" ] || exit 0
rules=$(printf '%s' "$result" | jq -r '.rules' 2>&1) || exit 0

cleaned=$(printf '%s' "$result" | jq -c '.ast' | shfmt --from-json 2>&1) || exit 0
# from-json terminates the output with a newline; command substitution
# strips it. Never emit an empty command.
[ -n "$cleaned" ] || exit 0
[ "$cleaned" = "$cmd" ] && exit 0

# Belt-and-braces no-statement-loss guard: re-parse the cleaned command and
# compare top-level statement counts. The transform never splices .Stmts
# (rules map over statements, edit within them, or prepend pipefail), so a
# rewrite can only ADD statements -- but if a future rule bug or a shfmt
# --from-json regression ever eats one, fail open (emit no rewrite) rather
# than execute a truncated command. The GUARD log line is the only trace,
# keeping the hook's silent contract.
orig_count=$(printf '%s' "$ast" | jq -e '.Stmts | length' 2>&1) || exit 0
cleaned_count=$(printf '%s' "$cleaned" | shfmt --to-json 2>&1 | jq -e '.Stmts | length' 2>&1) || exit 0
if [ "$cleaned_count" -lt "$orig_count" ]; then
	log_guard "$cmd" "$cleaned"
	exit 0
fi

log_rewrite "$cmd" "$cleaned" "$rules"

# updatedInput replaces tool_input wholesale, so echo back the ORIGINAL
# tool_input with only .command changed (preserves timeout, description, and
# any other fields). No permissionDecision: the normal permission flow keeps
# running against the rewritten command. No systemMessage, no
# additionalContext -- ever -- and suppressOutput hides the hook from the
# transcript entirely.
printf '%s' "$input" | jq -c --arg cmd "$cleaned" '{
	suppressOutput: true,
	hookSpecificOutput: {
		hookEventName: "PreToolUse",
		updatedInput: (.tool_input | .command = $cmd)
	}
}' || exit 0
