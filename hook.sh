#!/bin/sh
# The rule, the payload parse and the response all live in slopfmt. This exists
# only to fail OPEN when that binary is absent.
#
# A PreToolUse hook blocks the tool on any non-zero exit, so naming slopfmt
# directly in plugin.json would turn a missing binary into a guard that refuses
# every write in the session rather than none.
command -v "${SLOPFMT:-slopfmt}" >/dev/null 2>&1 || exit 0
exec "${SLOPFMT:-slopfmt}" hook --only tombstones \
	--max-comment-lines "${NO_TOMBSTONES_MAX_COMMENT_LINES:-14}"
