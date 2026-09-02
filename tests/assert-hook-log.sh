#!/usr/bin/env bash
# Asserts what the hook did during a real `claude` run, from its debug log.
# The e2e workflow drives claude and then invokes this; the assertions live here
# so an engineer can run them against a captured log without pushing a commit.
set -euo pipefail

log="${1:?usage: assert-hook-log.sh <hook-log-path>}"

if ! [ -s "$log" ]; then
	echo "FAIL: hook log '$log' is empty -- the hook never fired" >&2
	exit 1
fi

if ! grep -q 'REWRITE' "$log"; then
	echo "FAIL: hook log has no REWRITE entries" >&2
	exit 1
fi

# The rewrite strips the trailing | head and injects set -o pipefail.
if ! grep -qF 'cleaned="set -o pipefail\nseq 1 10"' "$log"; then
	echo "FAIL: expected the trailing | head stripped and pipefail injected" >&2
	exit 1
fi

echo "OK: hook stripped the trailing | head and injected pipefail"
