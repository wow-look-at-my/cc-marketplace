#!/bin/sh
# Approves read-only work, refuses a banned program. The event is on the payload.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" auto-allow
