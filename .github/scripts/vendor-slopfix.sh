#!/bin/sh
# Fetch the slopfix binary a plugin runs, and prove it answers before the plugin is allowed to ship.
set -eu

plugin_dir=${1:?usage: vendor-slopfix.sh <plugin-dir> [hook-rule...]}
shift

# An APE runs on every platform this marketplace targets.
url=${SLOPFIX_URL:-"https://dl.pazer.build/slopfix?os=linux&arch=amd64"}
# NOT build/.
build_dir="${plugin_dir}/bin"
binary="${build_dir}/slopfix.ape"

mkdir -p "$build_dir"
if ! curl -fL --compressed --no-progress-meter --connect-timeout 30 "$url" -o "$binary"; then
	echo "vendor-slopfix: could not download slopfix from ${url}" >&2
	exit 1
fi
chmod +x "$binary"

# The prologue is what `release-plugin` looks for.
magic=$(od -An -c -N 8 "$binary" | tr -d ' \n')
if [ "$magic" != "MZqFpD='" ]; then
	echo "vendor-slopfix: ${binary} is not an APE (first bytes: ${magic})" >&2
	exit 1
fi

# Every subcommand the manifest names has to exist in THIS binary.
manifest="${plugin_dir}/.claude-plugin/plugin.json"
if [ ! -f "$manifest" ]; then
	echo "vendor-slopfix: ${manifest} does not exist, so what this plugin calls is unknown." >&2
	exit 1
fi
named=$(tr -d '\n' <"$manifest" | grep -o 'slopfix\.ape [a-z][a-z-]*' | cut -d' ' -f2 | sort -u)
lsp_manifest="${plugin_dir}/.lsp.json"
if [ -f "$lsp_manifest" ]; then
	named=$(printf '%s\n%s\n' "$named" "$(jq -r '.[] | select(.command | endswith("/slopfix.ape")) | .args[0]' "$lsp_manifest")" | sort -u)
fi
for module in "${plugin_dir}"/hooks/*.ts; do
	[ -f "$module" ] || continue
	case "$module" in *.test.ts) continue ;; esac
	named=$(printf '%s\n%s\n' "$named" "$(grep -o "slopfix\.ape\`, '[a-z][a-z-]*'" "$module" | cut -d"'" -f2)" | sort -u)
done
if [ -z "$named" ]; then
	echo "vendor-slopfix: ${manifest} names no slopfix subcommand at all." >&2
	exit 1
fi
for name in $named; do
	if ! "$binary" "$name" --help >/dev/null 2>&1; then
		echo "vendor-slopfix: the manifest names '${name}', which this build of slopfix does not define." >&2
		echo "vendor-slopfix: the hook would run and answer nothing. Publish slopfix first." >&2
		exit 1
	fi
	echo "vendor-slopfix: ${name} is defined"
done

# The assertion that matters: run the real contract on text whose answer is
# known, and require the rule to fire.
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
	if ! answer=$(printf '%s' "$probe" | "$binary" check --json --path "$path" 2>&1); then
		echo "vendor-slopfix: the fetched slopfix cannot answer 'check --json --path ${path}':" >&2
		echo "  ${answer}" >&2
		echo "vendor-slopfix: the plugin calls a subcommand this build of slopfix does not have." >&2
		echo "vendor-slopfix: publish slopfix first -- a plugin whose checker cannot run must not ship." >&2
		exit 1
	fi
	case "$answer" in
	*"\"id\":\"$rule\""*) ;;
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

# One probe per rule family.
check_probe "yaml/comment-block" ".github/workflows/ci.yml" "$probe_workflow"
check_probe "ste/contraction" "docs/probe.md" "$probe_markdown"

# A plugin that drives the PreToolUse contract names the rules it runs, and each
# gets the same treatment: run it on text built to violate it, and require a
# verdict. Exit status alone proves only that the subcommand parses.
for rule in "$@"; do
	case "$rule" in
	counts)
		probe='This page has three sections.'
		suffix=md
		;;
	tombstones)
		probe='// This used to call the old resolver, which was removed.'
		suffix=go
		;;
	*)
		echo "vendor-slopfix: no probe defined for rule ${rule}. Add one -- an unprobed rule ships unproven." >&2
		exit 1
		;;
	esac

	payload=$(printf '{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/tmp/vendor-slopfix-probe.%s","content":"%s\\n"}}' \
		"$suffix" "$probe")

	if ! answer=$(printf '%s' "$payload" | "$binary" hook --only "$rule" 2>&1); then
		echo "vendor-slopfix: the fetched slopfix cannot answer 'hook --only ${rule}':" >&2
		echo "  ${answer}" >&2
		echo "vendor-slopfix: publish slopfix first -- a plugin whose guard cannot run must not ship." >&2
		exit 1
	fi
	case "$answer" in
	*hookSpecificOutput*) ;;
	*)
		echo "vendor-slopfix: 'hook --only ${rule}' ran and reported nothing on text that violates it." >&2
		echo "  probe:  ${probe}" >&2
		echo "  answer: ${answer:-<empty>}" >&2
		echo "vendor-slopfix: the guard would install and do nothing. Refusing to ship it." >&2
		exit 1
		;;
	esac
	echo "vendor-slopfix: ${rule} fired on its hook probe"
done

echo "vendor-slopfix: staged $(wc -c <"$binary") bytes at ${binary}"
