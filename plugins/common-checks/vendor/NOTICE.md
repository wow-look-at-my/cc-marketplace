# Vendored check modules

Fetched from [wow-look-at-my/actions](https://github.com/wow-look-at-my/actions) at commit `e54374f16c22c960ce2cc68232c7802797cfb010`.

Every file here is generated. Edit the check upstream, not this copy: the
plugin's build re-fetches these on every run and overwrites any local change.

## Covered

- `no-all-builds-job` -- src/detect.ts
- `yaml-comment-block` -- src/scan.ts
- `no-tests-in-yaml` -- src/scan.ts
- `push-excludes-tags` -- src/scan.ts
- `ste-lint` -- src/lint.ts, src/blocks.ts, src/ste100-banned-words.ts, src/guard.ts

## Runs in common-checks, reports nothing here

- `run-once` -- it claims the workflow run for one job. There is no rule a file can break.
