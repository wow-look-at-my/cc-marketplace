## Common Checks Plugin

The common-checks plugin lives at `plugins/common-checks/`. One **language server** that reports, on the line it sits on and while the
file is open, every violation that would fail the org's `common-checks` CI gate. It is the sibling of `css-duplication` in shape -- an
LSP rather than a hook, for the same reason: a finding that tracks the file's state disappears when the code is fixed, where a hook
shouts once per edit whether or not the finding is still true. It differs from every other plugin here in one way that decides its whole
design: **it contains no rules**. Every verdict comes out of the check's own module, fetched from
[wow-look-at-my/actions](https://github.com/wow-look-at-my/actions) at build time.

**Reusing the code is the point, and it is why this plugin is TypeScript in a marketplace of Go binaries.** A Go reimplementation of five
checkers would be a second source of truth: correct the day it was written, wrong the first time a regex changes upstream, and with
nothing to say so. The checks are TypeScript, so the server is TypeScript, bundled by `esbuild` into one file and started by a `/bin/sh`
launcher that finds Node. The cost is a Node runtime the other plugins do not need; the launcher states that out loud on stderr when it
cannot find one, because an LSP that fails to start reports nothing either way and the only difference a message makes is whether
`--debug lsp` explains the silence.

**Sync is enforced on the SET of checks, not only on their code.** `.github/scripts/vendor-common-checks/` resolves the upstream branch
to one commit, fetches `common-checks/action.yml`, reads its `uses:` list, and fails the build when that list and the plugin's `PLAN`
disagree in either direction -- a check added upstream that nothing here covers, or an entry here for a check upstream no longer runs.
Vendoring the modules alone would leave the plugin quietly enforcing four fifths of the gate. An entry may declare `files: []`, which is
how `run-once` is handled: it claims the workflow run for one job, and no open file can violate it. That entry is required rather than
optional, because the assertion cannot tell a decision from an omission. The vendor step runs from the plugin's `justfile` `prebuild` on
every CI build and a fetch failure fails the build, matching the `docs` plugin's Docker reference: packaging a silently stale checker is
the outcome the arrangement exists to avoid. `COMMON_CHECKS_REF` points the fetch at a branch, for building against a check that has not
merged yet.

**`vendor/` is generated. Do not edit it** -- a build overwrites it, and the header on every file says so along with the commit it came
from. The whole directory is replaced rather than merged, so a file the plan stopped producing goes away instead of lingering as a module
nothing imports and nothing refreshes.

**`src/checks.ts` adds exactly two things a check written for CI has no reason to produce.** A FILE KIND, since a check invoked from a
workflow already knows what it is reading and a server handed one open document does not: workflow files and action manifests for the
YAML checks, every markdown file for ste-lint, and nothing else -- firing on every `.yaml` is how a checker earns the reputation that gets
it uninstalled. And a LINE for `no-all-builds-job`, whose CI form names the job and never the line, because a CI annotation only ever had
a file to attach to. The verdict there is still upstream's; only the cursor position is worked out locally.

**Ranking decides what the model actually sees, because the client injects only the first handful of diagnostics per file.** ste-lint can
report hundreds of findings on one document while `push-excludes-tags` reports one, so the order is structural checks first and ste-lint
last. Two more things follow from the same budget: a hard-wrapped paragraph reports ONE finding at its first continuation line rather than
one per line (the same information, and it leaves room for everything else), and a file with more findings than the cap says so on the
last diagnostic it does send -- dropping the tail in silence would read as "that is all of it".

**Every diagnostic is severity Error, and the heuristic findings are not published at all.** ste-lint's warn buckets -- passive voice,
noun clusters, complex tense, dictionary word choice, long paragraphs -- never fail CI by their author's own deliberate design, so a
diagnostic for one would spend the budget on something the gate does not care about. What this plugin publishes means one thing: this
fails the merge gate.

- **Adapters**: `plugins/common-checks/src/checks.ts` -- file kind, the per-check calls into `vendor/`, the `no-all-builds-job` anchor,
  the ste-lint bucket wording and its wrapped-paragraph collapse, and the ranking
- **Server**: `plugins/common-checks/src/lsp.ts` -- base-protocol framing, the handshake (full document sync: a finding is a property of
  the whole document), didOpen/didChange/didSave/didClose, push and pull diagnostics, the per-file cap, and `relativize`
- **Entry point**: `plugins/common-checks/src/server.ts` -- serve stdio, nothing else
- **Launcher**: `plugins/common-checks/launcher.sh` -- staged into `build/` as `common-checks-lsp`, because the client execve()s the path
  in `.lsp.json` and a bundled `.js` is not executable on its own
- **Vendoring**: `.github/scripts/vendor-common-checks/plan.ts` (the plan, the drift assertion, the provenance header) and `main.ts` (the
  network and disk half, with the GitHub client shared with the `docs` plugin's vendoring rather than written twice)
- **Tests**: `src/checks.test.ts` (each check firing on the right line with a clean control beside it, the file-kind boundaries, the
  ranking, the wrapped-paragraph collapse, and a heuristic-only finding staying unreported), `src/lsp.test.ts` (the handshake, publish and
  clear, the explicit `null` shutdown result asserted on the RAW JSON keys, pull and push agreeing, the cap's overflow note, path
  resolution with and without a root, and the framing edge cases), `.github/scripts/vendor-common-checks/vendor.test.ts` (a check added
  and a check dropped upstream each failing the build by name, against a fake client that never touches the network)
- **Registration**: `plugins/common-checks/.lsp.json`

Adding a check upstream is meant to break this build. The failure names the check and the three ways out: vendor its module and write the
adapter, or add the entry with no files and say why nothing in an open file can violate it. Do not silence it by deleting the assertion --
the assertion is the feature.
