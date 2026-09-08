#!/bin/sh
# Refuses a loss, and a write that changes a file outside the edit tools.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" no-work-loss
