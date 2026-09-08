#!/bin/sh
# The permission decider. The rule is slopfix's `autoallow` package and its
# rules.xml, reached through `slopfix auto-allow`.
#
# It serves two events from one launcher. On PermissionRequest it approves
# read-only work, which is the event that fires only once the engine has landed
# on asking. On PreToolUse it refuses a program this environment does not run,
# which has to be judged on every call. The event is on the payload.
exec "$(dirname "$0")/../bin/slopfix.ape" auto-allow
