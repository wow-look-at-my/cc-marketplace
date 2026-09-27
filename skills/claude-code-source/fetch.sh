#!/bin/sh
# Puts the prettified Claude Code source for one version at
# /tmp/claude-docs-gaps-<version> and prints that path as the last line.
set -eu

repo=https://github.com/PazerOP/claude-docs-gaps
want=${1:-$(claude --version | awk '{print $1}')}

heads=$(git ls-remote --heads "$repo" '2.*' 2>&1) || {
	echo "fetch.sh: cannot read $repo:" >&2
	echo "$heads" | tail -n 2 >&2
	echo "In a web session the MAIN session must first call add_repo(owner=\"PazerOP\", repo=\"claude-docs-gaps\", access=\"read\"). A subagent has no add_repo, so it must stop and report this." >&2
	exit 1
}

# The highest branch at or below the wanted version, compared field by field.
have=$(printf '%s\n' "$heads" | sed 's|.*refs/heads/||' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' |
	{ cat; echo "$want"; } | sort -t. -k1,1n -k2,2n -k3,3n -u |
	awk -v w="$want" '$0 == w { print below; exit } { below = $0 }')
if printf '%s\n' "$heads" | grep -q "refs/heads/$want\$"; then
	have=$want
elif [ -n "$have" ]; then
	echo "fetch.sh: no branch for $want. Reading $have instead. Say so in the report." >&2
else
	echo "fetch.sh: no branch at or below $want." >&2
	exit 1
fi

dir=/tmp/claude-docs-gaps-$have
if [ ! -f "$dir/cli.js" ]; then
	git clone -q --depth 1 --single-branch --branch "$have" "$repo" "$dir"
fi
echo "$dir"
