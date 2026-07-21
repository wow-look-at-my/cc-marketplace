[private]
help:
    @just --list

# Hard-fail if anything in the plugin tree carries the bridge-key secret
# prefix (the pattern is assembled from parts so this recipe does not match
# itself). Env var NAMES are fine; secret VALUES are not.
# Scrub, then the full hook + MCP behavior matrix (fabricated stdin, poisoned
# curls -- never a live socket; tests/run-tests.ts).
prebuild:
    @pat=wdstate; if grep -rn "${pat}_" .; then echo "ERROR: bridge-key secret prefix found in plugin tree" >&2; exit 1; else echo "secret scrub clean"; fi
    node tests/run-tests.ts
