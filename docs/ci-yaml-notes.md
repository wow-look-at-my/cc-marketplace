# CI YAML notes

Rationale the workflow files point at, kept here so a `.yml` stays readable.
Each heading is the anchor a `# see docs/ci-yaml-notes.md#...` comment names.

## plugin-e2e-workflow-rationale

`plugin-e2e.yml` drives a real `claude` process with a plugin loaded. The Go
and bash unit suites assert what a hook or a server DOES with a given input;
they cannot assert that Claude Code registers it, spawns it, and uses its
answer. Every defect this workflow has caught lived in that gap: a malformed
JSON-RPC response only vscode-jsonrpc rejects, and a binary format only a real
`execve()` refuses.

One job per plugin whose contract is with the client, not with its own input.

## css-duplication-lsp-job

Builds the language server, cooks the plugin the way a release does, then runs
`claude --debug lsp --debug-file` over a stylesheet that already carries a
duplicated declaration block. The assertion step requires six lines in that log
(config loaded, process started, handshake finished, diagnostics published,
registered, delivered) and refuses two (a failed stop, a crash).

Cooking is load-bearing. `marketplace-build release-plugin` stages a `#!/bin/sh`
launcher at `build/<name>`, the path `.lsp.json` names, with the fat APE beside
it. Claude Code `execve()`s that path directly, and an APE is neither ELF nor a
`#!` script, so pointing the manifest at the raw APE fails with `undefined is
not an object (evaluating 'this.#handle')` and no server at all. Driving the
cooked tree is also what makes this job test the package that ships.

The prompt asks for a read-back after the edit: diagnostics drain into an
attachment on the NEXT turn, so a prompt that ends at the edit can finish
before delivery.

`--model sonnet` is deliberate on both jobs. Each asserts the behavior of a
hook or a server, which no model tier changes, and each runs on every push.

## go-toolchain-permission-grants

`wow-look-at-my/go-toolchain@v1` needs four grants, and fails without them:

- `id-token: write` — OIDC, for secret-server and buildhost.
- `contents: write` — it submits a dependency-graph snapshot; GitHub rejects
  the submission under `contents: read`.
- `actions: read` and `checks: read` — its embedded no-`all-builds` guard scans
  the run's jobs and the head commit's check runs, and fails closed when it
  cannot.

A job-level `permissions:` block REPLACES the workflow-level one, so a job that
declares its own must list all four.

## composite-action-caller-permissions

A composite action cannot request permissions; it runs with whatever the
calling job was granted. `setup-marketplace-build` runs `go-toolchain` when its
cache misses, so every job that uses it carries the four grants above even when
its own steps need none of them.

## marketplace-build-cache-miss

`setup-marketplace-build` builds only when its cache misses, so a warm entry
hides a break in that path for months. The install step is the backstop: it
fails naming the missing file rather than letting a later step discover it.

## release-build-binary-format-and-action-pin

`targets: cosmo` is not a size optimization: the fat APE is the only native
output the pinned action still emits, since the host-native build path was
removed from `v1`. One file covers Linux, macOS and Windows, and
`stageBinaries` (`tools/marketplace-build/ape_package.go`) turns it into the
shipping layout — the APE plus the launcher every manifest already points at.

`autorelease: 'false'` because plugins publish as git orphan tags, not to
buildhost; leaving it on would demand `deployments`/`artifact-metadata` write
for an upload nothing consumes.

## marketplace-json-replacement-and-a-stale-cache-key

`update-marketplace` writes `marketplace.json` from the cooked trees it is
given, replacing the file rather than patching it. A plugin whose tree is
absent — a cache hit that restored nothing, a build job that never ran —
therefore drops out of the published marketplace silently, and the next
`claude plugin update` for it 404s. The loop before that step fails the job
instead, naming every plugin with no cooked tree.

## smoke-test-job-rationale

Installs and updates EVERY plugin from the marketplace this run just published,
through real Claude Code. Each entry points at an orphan tag this run pushed, so
a tag that was never pushed, or one holding the wrong tree, fails here rather
than for the first person who installs it. It also asserts every source is an
anonymous `https://` clone: a `github` source clones over SSH, which works on a
machine with a key and fails for a user without one.
