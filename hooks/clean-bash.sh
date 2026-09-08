#!/bin/sh
# The Bash command cleaner. The rule is slopfix's `bashclean` package, reached
# through `slopfix clean-bash`.
#
# It rewrites a command rather than refusing it wherever it can: `rm` becomes
# `recycler trash`, a discarded stderr is put back, and an Actions read is
# spelled the way the shim accepts. Only a heredoc, an inline interpreter
# script and a partial file read are refused outright, each naming the tool
# that does the job instead.
exec "$(dirname "$0")/../bin/slopfix.ape" clean-bash
