## Common Checks Plugin

The common-checks plugin lives at `plugins/common-checks/`. It is one **language server**. It reports every violation that fails the org's `common-checks` CI gate, on the line that violation sits on, while the file is open.

It takes its shape from `css-duplication`, for the same reason. A finding that tracks the file's state disappears when the code is fixed. A hook shouts once per edit, whether or not the finding is still true.

**It differs from every other plugin here in one way, and that way decides its whole design. It contains no rules.** Every verdict comes out of the check's own module. The build fetches each module from [wow-look-at-my/actions](https://github.com/wow-look-at-my/actions).

### Why this plugin is TypeScript in a marketplace of Go binaries

Reuse of the real check code is the point. A Go reimplementation of five checkers is a second source of truth. It is correct on the day somebody writes it. It is wrong the first time a regex changes upstream. Nothing says so.

The checks are TypeScript. The server is TypeScript too. `esbuild` bundles it into one file. A `/bin/sh` launcher finds Node and starts it. The cost is a Node runtime that the other plugins do not need. The launcher states a missing Node out loud on stderr, because a language server that fails to start reports nothing either way. The only thing a message changes is whether `--debug lsp` explains the silence.

### Sync is enforced on the SET of checks, not only on their code

`.github/scripts/vendor-common-checks/` resolves the upstream branch to one commit. It fetches `common-checks/action.yml` and reads the `uses:` list out of it. The build fails when that list and the plugin's `PLAN` disagree in either direction. Two cases fail: a check added upstream that nothing here covers, and an entry here for a check that upstream no longer runs.

Vendoring the modules alone leaves the plugin quietly enforcing four fifths of the gate. That is the failure this assertion prevents.

An entry may declare `files: []` plus the reason. Such an entry is required rather than optional, because the assertion cannot tell a decision from an omission. Two checks use it, for the two different reasons a check reports nothing.

`run-once` has no rule an open file can break. It claims the workflow run for one job.

`push-excludes-tags` has a real rule, inline in a composite action, where nothing can import it. Making it importable meant converting it to a node action. That cost 642 lines against 34, on an action every repository in the org runs. 386 of those lines were a lockfile, for an eight-line rule. The gap is declared rather than paid for. Never close it by reimplementing the rule here.

The vendor step runs from the plugin's `justfile` `prebuild` recipe on every CI build. A fetch failure fails the build, which matches the `docs` plugin's Docker reference. Packaging a silently stale checker is the outcome this arrangement exists to avoid. `COMMON_CHECKS_REF` points the fetch at a branch, for a build against a check that has not merged yet.

**The `prepare` job resolves the upstream commit, and that resolution is load-bearing twice over.** It runs the same plan assertion, so a check added upstream fails the run before any plugin builds. It also feeds the plugin's cache key. Nothing under `plugins/common-checks/` changes when a rule changes upstream, so without it a cached build serves check code that CI no longer runs.

**`vendor/` is gitignored. That is the design rather than an oversight.** A copy of the checks in the tree is a second source of truth. It goes stale in silence, and nothing marks the moment it stops matching CI. It also puts prose the repository does not author in front of every check that reads the repository. The build fetches the modules and bundles them. Nothing is committed. Nothing can drift.

The whole directory is replaced on each fetch rather than merged. A file the plan stopped producing then goes away. It does not linger as a module nothing imports and nothing refreshes.

### What `src/checks.ts` adds

It adds exactly two things that a check written for CI has no reason to produce.

A **file kind**, because a check invoked from a workflow already knows what it reads, and a server handed one open document does not. Workflow files and action manifests go to the YAML checks. Every markdown file goes to ste-lint. Nothing else is judged. Firing on every `.yaml` is how a checker earns the reputation that gets it uninstalled.

A **line** for `no-all-builds-job`, whose CI form names the job and never the line. A CI annotation only ever had a file to attach to. The verdict there is still upstream's. Only the cursor position is worked out locally.

### Ranking, and the client's budget

Ranking decides what the model actually sees. The client injects only the first handful of diagnostics per file. ste-lint reports hundreds of findings on one document, and `no-all-builds-job` reports one. So the order puts the structural checks first and ste-lint last.

Two more things follow from the same budget. A hard-wrapped paragraph reports ONE finding at its first continuation line, rather than one per line. That is the same information. It leaves room for everything else. A file with more findings than the cap says so on the last diagnostic it sends. Dropping the tail in silence reads as a claim that the list is complete.

### Every diagnostic is severity Error

The heuristic findings are not published at all. ste-lint's warn buckets are passive voice, noun clusters, complex tense, dictionary word choice and long paragraphs. None of them fails CI, by their author's own deliberate design. A diagnostic for one spends the budget on something the gate does not care about. What this plugin publishes means one thing: this fails the merge gate.

### The registration, and what was checked rather than assumed

These facts were read out of the shipped bundle, per `/docs:claude-code-source`. The highest version carrying an extractable `cli.js` is 2.1.241. From 2.1.242 the npm package ships a native binary and `cli.js` is a stub. So these are 2.1.241's rules. A later change does not show up here.

- **`.lsp.json` at the plugin root is auto-discovered.** The manifest's `lspServers` key is read separately and merged into the same map. Either one works, and both together merge by server name. This plugin ships only the file, matching `css-duplication`.
- **`command` and `extensionToLanguage` are the two required keys**, and every extension must start with a dot. An entry missing either is dropped with an error rather than started. `diagnostics` defaults to true, and that default is what pushes `publishDiagnostics` into the agent's context after an edit. This plugin leaves it alone.
- **Three gates stop a server before it starts.** The plugin must be enabled. A `--plugin-dir` plugin counts as enabled unless somebody disables it. Safe mode disables `lspServers` outright. Bare mode and simple mode disable it unless a caller explicitly requests it. There is no print-mode gate in 2.1.241.
- **A fourth gate stops the FINDINGS rather than the server.** The diagnostic attachment is skipped entirely unless a Bash or PowerShell tool is in the session's tool set. A session without one gets no findings, silently, and this plugin cannot detect that.
- **One server per extension, and the first registered wins**, with a warning that names the loser. This plugin claims `.yml`, `.yaml` and `.md`. That is a wide claim. Another plugin that wants full YAML or markdown diagnostics cannot run beside it. That is a real tradeoff, not a gap to fix here.

**Live verification did not complete. The harness is the reason, rather than this plugin.** No plugin language server starts at all under `claude -p` with `--plugin-dir` on 2.1.261 in a headless session. The control was a plugin whose entire LSP command appends one line to a file. It never ran either, on a real `Edit` to a file with its registered extension. The diagnostics log carried `load_plugin_hooks_completed` and no LSP event of any kind.

The layer below that IS verified. A real LSP client drove the bundled `build/server.cjs` over stdio. It answered `initialize`, `didOpen` and `shutdown` correctly, and published all four workflow findings on the right lines. Run the interactive check before you trust the registration.

### Files

- **Adapters**: `plugins/common-checks/src/checks.ts` -- file kind, the per-check calls into `vendor/`, the `no-all-builds-job` anchor, the ste-lint bucket wording, the wrapped-paragraph collapse, and the ranking
- **Server**: `plugins/common-checks/src/lsp.ts` -- base-protocol framing, the handshake, the open and change and save and close notifications, push and pull diagnostics, the per-file cap, and `relativize`. Document sync is full, because a finding is a property of the whole document
- **Entry point**: `plugins/common-checks/src/server.ts` -- serve stdio, nothing else
- **Launcher**: `plugins/common-checks/launcher.sh` -- staged into `server/` as `common-checks-lsp`. The client execve()s the path in `.lsp.json`, and a bundled `.js` file is not executable on its own. The directory is `server/` and not `build/` because `release-plugin` requires every file under `build/` to be a fat APE, and this plugin ships no Go
- **Fetching**: `.github/scripts/vendor-common-checks/plan.ts` holds the plan, the drift assertion and the provenance header. `main.ts` holds the network and disk half, plus `--commit`. The GitHub client is shared with the `docs` plugin's fetcher rather than written twice
- **Tests**: `src/checks.test.ts` fires each check on the right line, with a clean control beside it. It also covers the file-kind boundaries, the ranking, the wrapped-paragraph collapse, and a heuristic-only finding staying unreported
- **Tests**: `src/lsp.test.ts` covers the handshake, publish and clear, pull and push agreeing, and the cap's overflow note. It also covers path resolution with and without a root, and the framing edge cases. It asserts the explicit `null` shutdown result on the RAW JSON keys
- **Tests**: the two suites under `.github/scripts/vendor-common-checks/` drive a fake client that never touches the network. A check added upstream and a check dropped upstream each fail the build by name
- **Registration**: `plugins/common-checks/.lsp.json`

Adding a check upstream is meant to break this build. The failure names the check and the ways out. Vendor its module and write the adapter, or add the entry with no files and say why no open file can violate it. Do not silence it by deleting the assertion. The assertion is the feature.
