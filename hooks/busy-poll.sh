#!/bin/sh
# Refuses a repeat that cannot learn anything. The event is on the payload.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" busy-poll
