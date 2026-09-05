# common-checks

A language server that reports, while you edit, the violations that would fail
the org's `common-checks` CI gate.

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install common-checks
```

Needs Node 18 or later on `PATH`, or `COMMON_CHECKS_NODE` set to a Node binary.

## What it reports

| File | Checks |
|------|--------|
| `.github/workflows/*.yml` | `push-excludes-tags`, `no-all-builds-job`, `yaml-comment-block`, `no-tests-in-yaml`, `ste-lint`'s continue-on-error guard |
| any `action.yml` | `yaml-comment-block`, `no-tests-in-yaml` |
| any `*.md` | `ste-lint` |

Every diagnostic is an error, because every one of them fails the merge gate.
`ste-lint`'s heuristic findings (passive voice, noun clusters, word choice) never
fail CI, so they are not reported.

## Where the rules live

Nowhere in this plugin. Each check's own module is fetched from
[wow-look-at-my/actions](https://github.com/wow-look-at-my/actions) on every
build and lands under `vendor/`. Change a rule upstream and the next release
carries it. `vendor/NOTICE.md` records the commit each file came from.

The build also reads `common-checks/action.yml` upstream and fails if the set of
checks it runs no longer matches the set this plugin covers.

## Configuration

| Variable | Effect |
|----------|--------|
| `COMMON_CHECKS_NODE` | Node binary the launcher runs |
| `COMMON_CHECKS_LSP_MAX_PER_FILE` | Diagnostics published per file, default 10 |
