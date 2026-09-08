#!/bin/sh
# The busy-poll guard. The rule is slopfix's `busypoll` package, reached
# through `slopfix busy-poll`, which reads the hook payload on stdin and writes
# the hook's own response on stdout.
#
# It serves two events from one launcher, because the rule is one question
# asked twice. On Stop it refuses a turn that repeated the same call at
# machine speed. On PreToolUse it refuses the status read itself, when this
# session already watched that pull request merge or that commit go green.
# Nothing about either answer can have changed in the seconds between calls.
#
# The event is on the payload, so nothing is passed here to say which is which.
exec "$(dirname "$0")/../bin/slopfix.ape" busy-poll
