[private]
help:
    @just --list

# Fetch the slopfix that carries every rule. It serves the language server and
# every hook. A fetch failure fails the build.
prebuild:
    cd ../.. && sh .github/scripts/vendor-slopfix.sh plugins/slopfix
