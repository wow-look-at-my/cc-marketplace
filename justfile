[private]
help:
	@just --list

# One Go binary serves all three events; go-toolchain builds and tests it.
build:

test: build

# Strip go-toolchain byproducts that would bloat the published tarball
# (the _host symlink dereferences to a duplicate binary at cook).
postbuild:
	rm -f build/*_host build/profile.json
