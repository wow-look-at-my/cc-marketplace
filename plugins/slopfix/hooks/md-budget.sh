#!/bin/sh
# Reports an instruction file over budget: nothing truncates what it costs.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" md-budget
