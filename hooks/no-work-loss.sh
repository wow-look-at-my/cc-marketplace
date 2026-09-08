#!/bin/sh
# The work-loss guard. The rule is slopfix's `noworkloss` package, reached
# through `slopfix no-work-loss`, which reads the PreToolUse payload on stdin
# and writes the hook's own response on stdout.
#
# It asks two questions of the same parsed command. Destruction: would this
# destroy content that exists only in the working tree? Provenance: does this
# change file content without going through Write, Edit or NotebookEdit?
exec "$(dirname "$0")/../bin/slopfix.ape" no-work-loss
