#!/bin/sh
# Fetch the slopfmt binary a guard plugin runs, and prove it answers before the
# plugin is allowed to ship.
#
# The plugin used to name `slopfmt` as a bare word and let PATH find it. The
# plugin and the binary then shipped on separate tracks with nothing checking
# they agreed: the plugin arrived from this marketplace per session, and the
# binary from `buildhost_install slopfmt`, unpinned. A plugin referencing a
# subcommand its installed binary predated saw a non-zero exit, and a PreToolUse
# hook blocks the tool on any non-zero exit. That shipped, and it blocked every
# write in a session carrying the plugin.
#
# Swallowing the exit code would have turned that into a guard that silently
# does nothing, which is worse: nothing reports it and nothing fixes it. So the
# binary ships INSIDE the plugin instead, and this script is the gate. A fetch
# failure, a wrong file, or a binary that cannot answer the payload all fail the
# build. A plugin whose guard does not work cannot be published.
set -eu

plugin_dir=${1:?usage: vendor-slopfmt.sh <plugin-dir> <rule> [extra-args...]}
rule=${2:?usage: vendor-slopfmt.sh <plugin-dir> <rule> [extra-args...]}
shift 2

# An APE runs on every platform this marketplace targets, so one file covers
# them all and `release-plugin` stages it as the plugin's own binary.
#
# SLOPFMT_URL points the fetch elsewhere, for a build against a slopfmt that has
# not published yet. It is also how the red control below is driven: the gate is
# worth nothing until somebody has watched it reject a binary that cannot answer.
url=${SLOPFMT_URL:-"https://dl.pazer.build/slopfmt?os=linux&arch=amd64"}
build_dir="${plugin_dir}/build"
binary="${build_dir}/slopfmt_cosmo_fat"

mkdir -p "$build_dir"
if ! curl -fL --compressed --no-progress-meter --connect-timeout 30 "$url" -o "$binary"; then
	echo "vendor-slopfmt: could not download slopfmt from ${url}" >&2
	exit 1
fi
chmod +x "$binary"

# The prologue is what `release-plugin` looks for. A gateway error page saved
# under this name would otherwise reach stageBinaries as a plausible file.
magic=$(od -An -c -N 8 "$binary" | tr -d ' \n')
if [ "$magic" != "MZqFpD='" ]; then
	echo "vendor-slopfmt: ${binary} is not an APE (first bytes: ${magic})" >&2
	exit 1
fi

# The assertion that matters: run the real hook contract on a payload whose
# answer is known, and require the rule to actually fire. Exit status alone
# proves only that the subcommand parses. A guard that runs and finds nothing in
# text built to violate it is the silent failure this whole arrangement exists
# to prevent, so the verdict is checked too.
case "$rule" in
counts)
	probe='This page has three sections.'
	;;
tombstones)
	probe='// This used to call the old resolver, which was removed.'
	;;
*)
	echo "vendor-slopfmt: no probe defined for rule ${rule}. Add one -- an unprobed rule ships unproven." >&2
	exit 1
	;;
esac

payload=$(printf '{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/tmp/vendor-slopfmt-probe.%s","content":"%s\\n"}}' \
	"$([ "$rule" = counts ] && echo md || echo go)" "$probe")

if ! answer=$(printf '%s' "$payload" | "$binary" hook --only "$rule" "$@" 2>&1); then
	echo "vendor-slopfmt: the fetched slopfmt cannot answer 'hook --only ${rule}':" >&2
	echo "  ${answer}" >&2
	echo "vendor-slopfmt: the plugin calls a subcommand this build of slopfmt does not have." >&2
	echo "vendor-slopfmt: publish slopfmt first -- a plugin whose guard cannot run must not ship." >&2
	exit 1
fi

case "$answer" in
*hookSpecificOutput*) ;;
*)
	echo "vendor-slopfmt: 'hook --only ${rule}' ran and reported nothing on text that violates it." >&2
	echo "  probe:  ${probe}" >&2
	echo "  answer: ${answer:-<empty>}" >&2
	echo "vendor-slopfmt: the guard would install and do nothing. Refusing to ship it." >&2
	exit 1
	;;
esac

echo "vendor-slopfmt: ${rule} fired on its probe ($(wc -c <"$binary") bytes)"
