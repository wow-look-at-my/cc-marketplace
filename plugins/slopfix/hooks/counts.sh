#!/bin/sh
# The counts repair. The rule, the payload parse and the response all live in
# slopfix. This runs the copy this plugin SHIPS, never one found on PATH.
#
# There is no probe and no swallowed exit code on purpose. Both were tried, and
# both were wrong. A PATH lookup let the plugin and the binary ship on separate
# tracks, so a plugin naming a subcommand its installed binary predated blocked
# every write in the session. Swallowing that exit code hid it instead, which
# left a guard that installs, reports success and does nothing.
#
# The build fetches the binary and proves it answers before the plugin is
# published, so absent and too-old cannot reach a session. What is left is a
# crash, and a crash must be loud.
#
# bin/ rather than build/: build/ holds the plugin's own Go hook binary, and
# `stageBinaries` keeps exactly one APE there and deletes the rest. A shell can
# exec an APE, so this needs no launcher between it and the file.
exec "$(dirname "$0")/../bin/slopfix.ape" hook --only counts
