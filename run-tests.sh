#!/bin/sh
# Run the plugin's TypeScript suite, and FAIL when it matched no tests.
#
# A bare `npx tsx --test plugins/<name>/src/*.test.ts` in a justfile is a silent
# green. After the plugin directory was renamed the glob matched nothing, node
# reported no tests at all, and the step still exited 0. A build that runs no
# tests must go red, so the count is checked here rather than trusted.
#
# Run from the repository root.
set -eu

src_dir=plugins/slopfix/src

# The glob is expanded here rather than by the caller, so an unmatched pattern
# is a countable zero instead of a literal word handed to node.
count=0
for f in "$src_dir"/*.test.ts; do
	[ -f "$f" ] || continue
	count=$((count + 1))
done

if [ "$count" -eq 0 ]; then
	echo "slopfix: no test files under ${src_dir}. A build that runs no tests must not pass." >&2
	exit 1
fi

echo "slopfix: running ${count} test files under ${src_dir}"
exec npx tsx --test "$src_dir"/*.test.ts
