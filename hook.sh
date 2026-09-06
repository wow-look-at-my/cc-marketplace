#!/bin/sh
# The rule, the payload parse and the response all live in slopfmt. This exists
# only to fail OPEN when that binary cannot answer.
#
# A PreToolUse hook blocks the tool on any non-zero exit. slopfmt carries its
# verdict in the JSON it prints and exits 0 whatever it decides, so a non-zero
# exit means it could not run: absent, too old to know the subcommand, or
# crashed. Each of those must let the write through rather than refuse it.
command -v "${SLOPFMT:-slopfmt}" >/dev/null 2>&1 || exit 0
"${SLOPFMT:-slopfmt}" hook --only counts || exit 0
