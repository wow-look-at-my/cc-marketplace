#!/bin/sh
# Marks a decision put to the reader in prose. Appends a line, refuses nothing.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" ask-properly
