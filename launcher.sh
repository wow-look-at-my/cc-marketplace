#!/bin/sh
# Staged into build/ as common-checks-lsp, because the LSP client execve()s the
# path in .lsp.json and a bundled .js is not executable on its own.
#
# A missing Node is stated out loud on stderr. An LSP server that cannot start
# reports nothing either way, so the only difference a message makes is whether
# `claude --debug lsp` explains the silence.
set -eu

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

for candidate in "${COMMON_CHECKS_NODE:-}" node nodejs; do
	[ -n "$candidate" ] || continue
	if command -v "$candidate" >/dev/null 2>&1; then
		exec "$candidate" "$dir/server.cjs" "$@"
	fi
done

echo "common-checks: no node on PATH, so the language server cannot start. Install Node 18 or later, or set COMMON_CHECKS_NODE to its path." >&2
exit 127
