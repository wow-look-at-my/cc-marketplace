#!/bin/sh
# The instruction-file budget. The rule is slopfix's `mdbudget` package,
# reached through `slopfix md-budget`.
#
# Every CLAUDE.md and every imported snippet is inlined into the prompt on
# every request, and nothing truncates them. The guard reports at session
# start, again the moment such a file is written, and blocks a Stop that
# leaves one over budget.
exec "$(dirname "$0")/../bin/slopfix.ape" md-budget
