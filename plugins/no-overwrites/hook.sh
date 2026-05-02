#!/usr/bin/env bash
# PreToolUse hook: blocks Write from overwriting an existing file.
# Reads the hook event JSON from stdin. Exits 2 with a stderr message to block.

file_path=$(jq -r 'if .tool_name == "Write" then .tool_input.file_path // empty else empty end' 2>/dev/null) || exit 0

if [ -z "$file_path" ] || [ ! -e "$file_path" ]; then
    exit 0
fi

printf 'BLOCKED: Cannot overwrite existing file "%s" with Write tool. Use the Edit tool instead to make changes to existing files.' "$file_path" >&2
exit 2
