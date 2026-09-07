#!/bin/sh
# The rule, the payload parse and the response all live in slopfmt. This runs
# the copy this plugin SHIPS, never one found on PATH.
#
# There is no probe and no swallowed exit code on purpose. Both were here, and
# both were wrong. A PATH lookup let the plugin and the binary ship on separate
# tracks, so a plugin naming a subcommand its installed binary predated blocked
# every write in the session. Swallowing that exit code hid it instead, which
# left a guard that installs, reports success and does nothing.
#
# The build fetches the binary and proves it answers before the plugin is
# published, so absent and too-old cannot reach a session. What is left is a
# crash, and a crash must be loud.
exec "$(dirname "$0")/build/no-counts-in-docs" hook --only counts
