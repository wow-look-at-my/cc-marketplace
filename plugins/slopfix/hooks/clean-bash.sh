#!/bin/sh
# Rewrites a Bash command rather than refusing it, wherever a rewrite exists.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" clean-bash
