[private]
help:
	@just --list

# Strip go-toolchain byproducts that would bloat the published tarball
# (the _host symlink dereferences to a ~8.5MB duplicate binary at cook).
postbuild:
	rm -f build/*_host build/profile.json
