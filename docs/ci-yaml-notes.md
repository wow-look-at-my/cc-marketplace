# CI YAML notes

Rationale the workflow files point at, kept here so a `.yml` stays readable.
Each heading is the anchor a `# see docs/ci-yaml-notes.md#...` comment names.

## plugin-e2e-jobs

The `e2e-*` jobs drive a real `claude` process with a plugin loaded. The Go
and bash unit suites assert what a hook or a server DOES with a given input;
they cannot assert that Claude Code registers it, spawns it, and uses its
answer. Every defect these jobs have caught lived in that gap: a malformed
JSON-RPC response only vscode-jsonrpc rejects, and a binary format only a real
`execve()` refuses.

One job per plugin whose contract is with the client, not with its own input.

They live in `release.yml` rather than a workflow of their own. Both triggered
on the same push, so a second workflow bought nothing but a second checkout, a
second required-check surface, and two places to keep the go-toolchain ref in
step. Nothing needs them: they carry no `needs:`, so they start immediately and
do not gate publishing, exactly as they did as a separate workflow.

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

`wow-look-at-my/go-toolchain@lkgb` needs four grants, and fails without them:

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
output the action still emits, since the host-native build path was removed
from go-toolchain. The ref is `@lkgb`, that repository's last-known-good-build
tag: the `v1` branch every workflow used to name no longer exists there, and
`@master` would move under every run. One file covers Linux, macOS and Windows, and
`stageBinaries` (`tools/marketplace-build/ape_package.go`) turns it into the
shipping layout — the APE plus the launcher every manifest already points at.

`autorelease: 'false'` because plugins publish as git orphan tags, not to
buildhost; leaving it on would demand `deployments`/`artifact-metadata` write
for an upload nothing consumes.

## plugin-tree-hand-off

Each cooked tree travels from its `build` matrix leg to `publish-marketplace`
inside the same run, which is what `wow-look-at-my/actions@cache-upload#latest`
is for — GitHub bills artifact storage and does not bill cache storage. The
consumer side has one wrinkle. `cache-download` restores a SINGLE named
hand-off; it has no `pattern:` like `actions/download-artifact`, and a workflow
cannot repeat a `uses:` step over a list whose length is decided at runtime by
`prepare`. So `publish-marketplace` checks the action out (its repository is
public) and runs its bundle once per plugin, passing `name` and `path` as the
`INPUT_*` variables the runner would set. It is the same code the `uses:` form
runs, driven by a loop.

Do not replace the loop with a fixed set of download steps. The plugin list is
whatever `prepare-matrix` finds, and a hard-coded list silently drops a plugin
added later — the exact failure the section below describes.

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
