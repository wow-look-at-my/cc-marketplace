[private]
help:
    @just --list

# Fetch the slopfix that carries every rule, and bundle the server, so a release
# enforces what common-checks enforces today. A fetch failure fails the build:
# shipping a plugin with no checker in it is what this arrangement avoids.
#
# The suite runs through run-tests.sh rather than a bare glob. A glob that
# matches nothing reports no tests and still exits 0, which is how the rename
# shipped a green build that checked nothing.
prebuild:
    cd ../.. && sh .github/scripts/vendor-slopfix.sh plugins/slopfix
    cd ../.. && sh plugins/slopfix/run-tests.sh
    mkdir -p server
    cd ../.. && npx esbuild plugins/slopfix/src/server.ts --bundle --platform=node --target=node18 --format=cjs --outfile=plugins/slopfix/server/server.cjs
    cp launcher.sh server/slopfix-lsp
    chmod +x server/slopfix-lsp
