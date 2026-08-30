[private]
help:
	@just --list

# Strip go-toolchain byproducts that would bloat the published tarball
# (the _host symlink dereferences to a duplicate binary at cook time).
postbuild:
	rm -f build/*_host build/profile.json
