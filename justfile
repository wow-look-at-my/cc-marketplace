[private]
help:
    @just --list

# Re-fetch the check modules and bundle the server, so a release enforces what
# common-checks enforces today. A fetch failure fails the build: shipping a
# silently stale checker is what this arrangement exists to avoid.
prebuild:
    cd ../.. && npx tsx .github/scripts/vendor-common-checks/main.ts
    cd ../.. && npx tsx --test plugins/common-checks/src/*.test.ts
    mkdir -p server
    cd ../.. && npx esbuild plugins/common-checks/src/server.ts --bundle --platform=node --target=node18 --format=cjs --outfile=plugins/common-checks/server/server.cjs
    cp launcher.sh server/common-checks-lsp
    chmod +x server/common-checks-lsp
