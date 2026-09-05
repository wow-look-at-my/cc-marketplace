# common-checks

A language server that reports, while you edit, the violations that fail the org's `common-checks` CI gate.

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

Every diagnostic is an error, because every one of them fails the merge gate. The heuristic findings of `ste-lint` never fail CI, so this plugin does not report them. Those are passive voice, noun clusters and word choice.

## Where the rules live

Nowhere in this plugin. Every build fetches each check's own module from [wow-look-at-my/actions](https://github.com/wow-look-at-my/actions) into `vendor/`. Change a rule upstream and the next release carries it. `vendor/NOTICE.md` records the commit each file came from.

The build also reads `common-checks/action.yml` upstream. It fails when the set of checks that file runs no longer matches the set this plugin covers.

## Configuration

| Variable | Effect |
|----------|--------|
| `COMMON_CHECKS_NODE` | Node binary the launcher runs |
| `COMMON_CHECKS_LSP_MAX_PER_FILE` | Diagnostics published per file, default 10 |
