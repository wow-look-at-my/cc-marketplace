# CI YAML notes

Reference notes for `.github/workflows/*.yml` and `.github/actions/*/action.yml` that were too
long to carry as inline comments -- the org's `yaml-comment-block` check fails any GitHub Actions
YAML file that carries more than 1 comment line in a row.

## Composite action caller permissions

`setup-marketplace-build` (`.github/actions/setup-marketplace-build/action.yml`) runs
`go-toolchain` on a cache miss. A composite action cannot request its own permissions, so every
job that uses it must declare go-toolchain's full permission set itself:

```yaml
permissions:
  id-token: write
  contents: write   # go-toolchain submits the dependency graph; 403 without it
  actions: read     # its no-all-builds guard scans the run's jobs...
  checks: read      # ...and the head commit's check runs; both fail closed
```

The `go-toolchain` step inside the composite runs only on a cache MISS -- only when
`tools/marketplace-build` changes -- so a job with an incomplete permission set stays green for
months and then fails the first build that touches the tool. `release.yml`'s `prepare` and
`publish-marketplace` jobs both use this composite and carry this same permission set for this
reason.

## go-toolchain permission grants

Any job that invokes `wow-look-at-my/go-toolchain@v1` directly (not through the composite above)
needs the same grants for a related reason: go-toolchain submits the module/dependency graph to
GitHub, which the API gates behind `contents: write`, and its no-all-builds guard scans the run's
jobs (`actions: read`) and the head commit's check runs (`checks: read`), failing closed without
either. `release.yml`'s `build` job and `plugin-e2e.yml`'s `css-duplication-lsp` job both declare
a job-level `permissions:` block for this reason -- a job-level block REPLACES the workflow-level
one, so every permission the job needs must be listed there, not assumed from the top of the file.

## Release build binary format and action pin

- **One fat APE, not an os x arch matrix.** A plugin ships exactly one binary now, and
  `release-plugin` FAILS CLOSED on a `build/` directory holding per-platform binaries instead
  (`ape_package.go`), rather than shipping a package that works on some platforms and silently
  not others.
- **`@v1`, not `@latest`.** `latest` is a frozen orphan tag from 2026-05 that predates the
  `targets` input, and an unknown `with:` key is a WARNING, not an error -- so `targets: cosmo`
  was silently dropped and the default matrix ran instead. `v1` is the ref the org documents and
  every other repo uses.

## Marketplace json replacement and a stale cache key

`update-marketplace`'s `plugins` array is REPLACED wholesale from the combined-plugins input in
`publish-marketplace`. A plugin missing from that input silently vanishes from the marketplace --
users can no longer install it, and every check still stays green -- so the `Prepare combined
input` step fails the job instead when one is missing.

This is also the reason `prepare`'s build/cached split does not currently save any CI time: its
cache-key lookup looks for a key that never matches the one the `build` job's `Save plugin cache`
step writes, so every plugin is rebuilt on every run today. The missing-plugin check above is
what will catch the day that changes, if a stale cached plugin ever silently drops out instead.

## Smoke test job rationale

`smoke-test` drives real Claude Code against the marketplace this run just published, and
actually installs and updates every plugin. It is the only job that exercises the install path a
real user takes: the demo workflow loads a local `--plugin-dir`, and the `build` job only runs
`claude plugin validate` against a cooked directory. Every marketplace entry now names a git ref,
so this job is what proves each orphan tag was really pushed and really holds an installable
plugin -- a tag the `build` job failed to push shows up as a 404 here, rather than in production.

## Plugin e2e workflow rationale

Each job in `plugin-e2e.yml` drives real Claude Code against one plugin and asserts the plugin
actually did its job. `claude plugin validate` (in `release.yml`) only checks a manifest -- it
never starts a hook or a language server, which is how a broken LSP shutdown once reached
`master` while every check stayed green.

### css-duplication-lsp job

The css-duplication plugin ships a language server, and nothing else in CI ever starts one:
`claude plugin validate` reads the manifest, and the `smoke-test` job (`release.yml`) installs
plugins without exercising them. This job runs the whole chain the way a user does -- Claude Code
loads `.lsp.json`, spawns the binary, edits a stylesheet into a duplicate, and the server's
diagnostics come back as an attachment -- then asserts the LSP session also SHUT DOWN cleanly,
which is the specific failure a green test suite hid: a malformed JSON-RPC response is only
rejected by a real client.
