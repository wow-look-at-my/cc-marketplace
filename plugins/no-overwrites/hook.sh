#!/usr/bin/env bash
# PreToolUse hook: blocks Write from overwriting an existing file, and blocks
# Write to a path that is currently sitting in the recycle bin.
# Reads the hook event JSON from stdin. Exits 2 with a stderr message to block.
#
# The second case closes the delete-then-Write loophole around the first. A
# Write to an existing path is refused here, which makes "delete the path,
# then Write it" the obvious way around the block -- and in that window the
# only copy of the file's content is in the model's context, so a compaction
# or an interrupted turn destroys it with nothing on disk to recover from.
# Since `rm` is rewritten to `recycler trash` (see the cleanup-bash-cmds
# plugin), the bytes are still in the recycle bin: recycler tracks each
# item's original location, so this can name the exact restore command
# instead of reporting an unrecoverable loss.

set -euo pipefail

file_path=$(jq -r 'if .tool_name == "Write" then .tool_input.file_path // empty else empty end' 2>/dev/null) || exit 0

if [ -z "$file_path" ]; then
    exit 0
fi

if [ -e "$file_path" ]; then
    printf 'BLOCKED: Cannot overwrite existing file "%s" with Write tool. Use the Edit tool instead to make changes to existing files.' "$file_path" >&2
    exit 2
fi

# The path does not exist. If it is in the recycle bin, restoring it is
# strictly better than re-authoring it from context. recycler stores the
# original path resolved (on macOS /tmp/x is recorded as /private/tmp/x), so
# compare the physical path too -- resolve the parent directory, which
# exists even though the file itself does not.
command -v recycler >/dev/null || exit 0

phys_path=$file_path
parent=$(dirname -- "$file_path")
if [ -d "$parent" ]; then
    phys_parent=$(cd -- "$parent" && pwd -P) || phys_parent=""
    if [ -n "$phys_parent" ]; then
        phys_path="${phys_parent%/}/$(basename -- "$file_path")"
    fi
fi

# jq exits 1 when no item matches; a broken/absent bin must never block a
# legitimate Write, so any failure here falls through to allow.
trashed=$(recycler list --json |
    jq -er --arg p "$file_path" --arg q "$phys_path" \
        'map(select((.original_path // "") == $p or (.original_path // "") == $q)) | .[0].original_path') || exit 0

[ -n "$trashed" ] || exit 0

printf 'BLOCKED: "%s" is not missing -- it is in the recycle bin, and Write would author a fresh file over the top of it, losing whatever was in the original.\n\nRestore it and edit instead:\n  recycler restore %s\n\nThen use the Edit tool. If you genuinely want to discard the recycled copy and start over, say so explicitly first.' \
    "$file_path" "$trashed" >&2
exit 2
