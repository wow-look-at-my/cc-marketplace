#!/bin/sh
# The tombstones repair. The rule, the payload parse and the response all live
# in slopfix. This runs the copy this plugin SHIPS, never one found on PATH.
#
# There is no probe and no swallowed exit code on purpose, for the reasons
# counts.sh records beside it.
#
# The volume cap stays an environment knob rather than a plugin option. A
# plugin option reaches a hook as CLAUDE_PLUGIN_OPTION_<KEY>, so moving it is a
# one-word change here, but it changes the name a user already sets.
exec "$(dirname "$0")/../bin/slopfix.ape" hook --only tombstones \
	--max-comment-lines "${NO_TOMBSTONES_MAX_COMMENT_LINES:-14}"
