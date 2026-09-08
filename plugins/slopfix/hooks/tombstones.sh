#!/bin/sh
# Strips a tombstone. The cap is an env knob because users already set it.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" \
	hook --only tombstones \
	--max-comment-lines "${NO_TOMBSTONES_MAX_COMMENT_LINES:-14}"
