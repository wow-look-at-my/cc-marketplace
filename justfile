[private]
help:
	@just --list

# Strip go-toolchain byproducts that would bloat the published tarball.
postbuild:
	rm -f build/*_host build/profile.json
