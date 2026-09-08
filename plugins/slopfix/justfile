[private]
help:
    @just --list

# Fetch the slopfix that carries every rule, and bundle the server, so a release
# enforces what common-checks enforces today. A fetch failure fails the build:
# shipping a plugin with no checker in it is what this arrangement avoids.
prebuild:
    cd ../.. && sh .github/scripts/vendor-slopfix.sh plugins/common-checks
    cd ../.. && npx tsx --test plugins/common-checks/src/*.test.ts
    mkdir -p server
    cd ../.. && npx esbuild plugins/common-checks/src/server.ts --bundle --platform=node --target=node18 --format=cjs --outfile=plugins/common-checks/server/server.cjs
    cp launcher.sh server/common-checks-lsp
    chmod +x server/common-checks-lsp
