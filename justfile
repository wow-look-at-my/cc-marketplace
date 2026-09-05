[private]
help:
	@just --list

# identshape_gen.go is committed, because a released plugin builds without
# go-regex-compiler on the runner. This recipe is what keeps the committed copy
# honest: it regenerates into a temporary file and fails when the two differ, so
# an edited pattern that nobody re-ran cannot reach master looking current.
prebuild:
	#!/usr/bin/env bash
	set -euo pipefail
	if ! command -v go-regex-compiler >/dev/null; then
		curl -fsSL "https://dl.pazer.build/go-regex-compiler?os=linux&arch=amd64" \
			-o /usr/local/bin/go-regex-compiler
		chmod +x /usr/local/bin/go-regex-compiler
	fi
	go-regex-compiler \
		--regex '\b[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+\b|\b[a-z][a-z0-9]*[A-Z][A-Za-z0-9]*\b|\b[A-Z][a-z0-9]+[A-Z][A-Za-z0-9]*\b' \
		--match full --func matchesIdentifierShape --package main \
		--output identshape_gen.go.check
	if ! diff -u identshape_gen.go identshape_gen.go.check; then
		rm -f identshape_gen.go.check
		echo "identshape_gen.go is stale: re-run 'just prebuild' and commit the result" >&2
		exit 1
	fi
	rm -f identshape_gen.go.check

# Strip go-toolchain byproducts that would bloat the published tarball.
postbuild:
	rm -f build/*_host build/profile.json
