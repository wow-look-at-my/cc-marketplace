[private]
help:
    @just --list

# Re-fetch the Docker reference, so a release carries current text rather than the day it was committed.
# A fetch failure fails the build: packaging a silently stale reference is what this exists to avoid.
prebuild:
    cd ../.. && npx tsx .github/scripts/vendor-docker-docs/main.ts
