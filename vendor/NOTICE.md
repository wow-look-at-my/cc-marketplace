# Vendored check modules

Fetched from [wow-look-at-my/actions](https://github.com/wow-look-at-my/actions) at commit `ac445afee81e9faea81cf68d088348ffa9d80dce`.

Every file here is generated. Edit the check upstream, not this copy: the
plugin's build re-fetches these on every run and overwrites any local change.

## Covered

- `no-all-builds-job` -- src/detect.ts
- `yaml-comment-block` -- src/scan.ts
- `no-tests-in-yaml` -- src/scan.ts
- `ste-lint` -- src/lint.ts, src/blocks.ts, src/ste100-banned-words.ts, src/guard.ts

## Runs in common-checks, reports nothing here

- `run-once` -- it claims the workflow run for one job. There is no rule a file can break.
- `push-excludes-tags` -- its rule is an inline script inside a composite action, which nothing can import. Converting it to a node action to expose one eight-line rule cost 642 lines against 34, 386 of them a lockfile, on an action every repository in the org runs.
