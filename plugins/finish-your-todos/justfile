[private]
help:
	@just --list

# All three hooks are one Go binary, built and tested by go-toolchain.
build:

test: build

# Strip go-toolchain byproducts that would bloat the published tarball
# (the _host symlink dereferences to a duplicate binary at cook).
postbuild:
	rm -f build/*_host build/profile.json
