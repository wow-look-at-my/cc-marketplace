#!/bin/sh
# Fetch the slopfix binary a plugin runs, and prove it answers before the plugin
# is allowed to ship.
#
# A plugin that names its binary as a bare word and lets PATH find it travels on
# a separate track from that binary: a plugin calling a subcommand its installed
# binary predates reports nothing at all, and nothing says so. Swallowing the
# failure is worse again -- that leaves a guard which installs, reports success
# and does nothing.
#
# So the binary ships INSIDE the plugin, and this script is the gate. A fetch
# failure, a wrong file, or a binary that cannot answer the contract all fail
# the build.
#
# This was vendor-slopfmt.sh, naming the project slopfmt. buildhost still serves
# that name, so a fetch of it succeeds and hands back a binary frozen before the
# rename. The `report` probe below is what refuses one: that subcommand exists
# only after the rename, so an old build cannot pass this gate quietly.
set -eu

plugin_dir=${1:?usage: vendor-slopfix.sh <plugin-dir> [hook-rule...]}
shift

# An APE runs on every platform this marketplace targets, so one file covers
# them all and `release-plugin` stages it as the plugin's own binary.
#
# SLOPFIX_URL points the fetch elsewhere, for a build against a slopfix that has
# not published yet. It is also how the red control is driven: the gate is worth
# nothing until somebody has watched it reject a binary that cannot answer.
url=${SLOPFIX_URL:-"https://dl.pazer.build/slopfix?os=linux&arch=amd64"}
build_dir="${plugin_dir}/build"
binary="${build_dir}/slopfix_cosmo_fat"

mkdir -p "$build_dir"
if ! curl -fL --compressed --no-progress-meter --connect-timeout 30 "$url" -o "$binary"; then
	echo "vendor-slopfix: could not download slopfix from ${url}" >&2
	exit 1
fi
chmod +x "$binary"

# The prologue is what `release-plugin` looks for. A gateway error page saved
# under this name would otherwise reach stageBinaries as a plausible file.
magic=$(od -An -c -N 8 "$binary" | tr -d ' \n')
if [ "$magic" != "MZqFpD='" ]; then
	echo "vendor-slopfix: ${binary} is not an APE (first bytes: ${magic})" >&2
	exit 1
fi

# The assertion that matters: run the real contract on text whose answer is
# known, and require the rule to actually fire. Exit status alone proves only
# that the subcommand parses. A binary that runs and finds nothing in text built
# to violate a rule is the silent failure this arrangement exists to prevent.
probe_workflow='name: CI
# one
# two
# three
on: push
'
probe_markdown="This shouldn't run; it is banned.
"

check_probe() {
	rule=$1
	path=$2
	probe=$3
	if ! answer=$(printf '%s' "$probe" | "$binary" report --path "$path" 2>&1); then
		echo "vendor-slopfix: the fetched slopfix cannot answer 'report --path ${path}':" >&2
		echo "  ${answer}" >&2
		echo "vendor-slopfix: the plugin calls a subcommand this build of slopfix does not have." >&2
		echo "vendor-slopfix: publish slopfix first -- a plugin whose checker cannot run must not ship." >&2
		exit 1
	fi
	case "$answer" in
	*"\"$rule\""*) ;;
	*)
		echo "vendor-slopfix: '${rule}' reported nothing on text that violates it." >&2
		echo "  path:   ${path}" >&2
		echo "  answer: ${answer:-<empty>}" >&2
		echo "vendor-slopfix: the plugin would install and check nothing. Refusing to ship it." >&2
		exit 1
		;;
	esac
	echo "vendor-slopfix: ${rule} fired on its probe"
}

# One probe per rule family the plugin reports. A workflow rule and a prose rule
# reach slopfix down different paths, so one probe proves only half of it.
check_probe "yaml/comment-block" ".github/workflows/ci.yml" "$probe_workflow"
check_probe "ste/contraction" "docs/probe.md" "$probe_markdown"

echo "vendor-slopfix: staged $(wc -c <"$binary") bytes at ${binary}"
