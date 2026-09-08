#!/bin/sh
# Renders a reference as a markdown link. The lookup rides GH_HOST.
exec "${CLAUDE_PLUGIN_ROOT:?only a plugin hook has one}/bin/slopfix.ape" link-refs
