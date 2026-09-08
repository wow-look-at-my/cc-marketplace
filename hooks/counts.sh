#!/bin/sh
# Cuts a cardinal out of a document. No probe, no swallowed exit: a crash is loud.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" hook --only counts
