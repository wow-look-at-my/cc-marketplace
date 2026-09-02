## No Work Loss Plugin

The no-work-loss plugin lives at `plugins/no-work-loss/`. One dependency-light Go binary on a **PreToolUse/Bash** hook, refusing any command
that would destroy content which exists only in the working tree. The invariant is narrow on purpose: committed work is reachable from the
reflog, so history rewrites are not its business; a modified-but-uncommitted or untracked file is in no git object and cannot be recovered
once overwritten. Denials emit `permissionDecision: "deny"` with a reason carrying the counts, the filenames, and the exact safe command
(`blocked: git reset --hard would lose 1 modified file (app.go).` / `run: git stash push -u -m "pre-reset" && ...`); everything else writes
nothing at all, leaving the normal permission flow untouched.

**Hazard classes are kept apart, and that is the load-bearing design decision.** The obvious simplification -- one "is the tree dirty" bit --
is wrong, because `reset --hard` destroys tracked modifications and spares untracked files while `clean -fd` does exactly the reverse, and
`stash drop` destroys neither. A boolean makes each verb refuse over the half it will not touch, which is how a guard earns the reputation
that gets it uninstalled. Each verb is therefore checked only against the classes it can reach (`hazTracked` / `hazUntracked` / `hazIgnored`
/ `hazStash`), and `TestResetHardSparesUntrackedFiles` + `TestCleanSparesTrackedModifications` pin both halves.

**Ref-destroying verbs ask a different question: does this content exist anywhere else?** (`reach.go`) Deleting a merged branch,
force-pushing when the old tip is still on another branch, and dropping a remote branch already on master all lose nothing, so they are
ALLOWED -- recoverable content is not protected content. `branch -D`/`-M`, `push --force`/`+refspec`/`--delete` and `update-ref -d` resolve
the tip and ask whether another ref contains it (a push additionally allows a fast-forward, where nothing is rewritten); `filter-branch`
requires HEAD fully pushed; `reflog expire`/`delete` requires `fsck --unreachable --no-reflogs` to find no commit; `worktree remove --force`
probes that worktree's own status. Only `push --mirror` stays an unconditional deny -- it rewrites every ref at once, so no bounded set of
commits can be checked. This does NOT extend to the working tree: a modified or untracked file's bytes are in no commit by definition, so
"already pushed" is never true of them, and `git stash drop` sits with them because stashing is the act of parking content off every branch.

Two facts here were established by RUNNING git, and each had already produced a wrong verdict in a draft. **`--exclude` does not take a full
refname** -- for `--branches`/`--remotes` it matches the name without the `refs/heads/`/`refs/remotes/` prefix, so
`--exclude=refs/heads/feature --branches` excludes nothing and a branch holding the only copy of a commit reported "0 lost", a silent false
negative. **`refs/remotes/<remote>/HEAD` is a symbolic alias** for the branch being overwritten, and counting it made every force push look
safe. Hence containment via `for-each-ref --contains` with remote-HEAD filtered out, rather than hand-built exclusion lists. A missing
remote-tracking ref denies (`errNoRemoteRef`, "git fetch first"): absence of a local mirror is not evidence the remote is empty. A missing
LOCAL ref allows, since git will error on its own -- conflating those two was itself a caught bug.

**Detection parses; it does not match substrings.** `mvdan.cc/sh/v3/syntax` (the same parser `enhanced-auto-allow` uses) flattens the command
into every unit that executes -- `&&`/`||`/`;`/pipes/newlines, subshells, brace blocks, `if`/`while`/`for`/`case`/function bodies, and
command substitutions. A `cd` is tracked across the sequence and the cwd pointer is shared **only where the shell shares one**: `&&`/`||`/`;`
and brace blocks carry it forward, a pipe stage, subshell and conditional body each get a copy. `git -C` and `--work-tree` apply the same
way. Wrappers resolve (`env`, `sudo`, `doas`, `command`, `builtin`, `exec`, `nohup`, `nice`, `ionice`, `setsid`, `stdbuf`, `timeout`,
`xargs`, leading `\`, absolute paths) with their value-taking flags understood -- `nice -n 10 git reset --hard` otherwise leaves `10` where
the program should be and the git behind it is never seen, which was a real bug the wrapper tests caught. Short flags unbundle, so `-fdx`,
`-xdf` and `-f -d -x` are one command. Aliases resolve out of `git config --get-regexp '^alias\.'`, including the `!shell` form (re-parsed as
shell); builtin verbs skip the lookup because git refuses to let an alias shadow one, chains resolve, and self-reference terminates at depth 3.

**Ambiguity denies**: an unparseable command that names a destructive verb, an operand that is not statically known (`rm $TARGET`, `cd $DIR
&& git reset --hard`, `cd -`), or `GIT_DIR`/`GIT_WORK_TREE`/`--git-dir` relocating the repo away from the path words. An unparseable command
with nothing destructive in it is allowed.

**Fail-safety is honest about its one gap.** In-process, a panic is recovered into a denial, and a git subprocess that errors or exceeds the
3s timeout produces "cannot tell whether ... would lose uncommitted work" rather than an assumption of clean. But Claude Code's own docs state
a hook that times out does NOT block the call ("don't count on a stalled hook to act as a gate"), so a killed or hung binary lets the command
through -- the 3s internal timeout exists to stay well inside the harness window so this plugin's deny path is the one that fires. A hook
cannot make itself mandatory; this is platform behavior, not an unhandled case.

The allow list matters as much as the deny list, and is tested as heavily: `checkout -b`/`switch -c`, `stash push`, `commit`, `add`,
`restore --staged` (index only, file stays on disk), appends (`>>`, `tee -a`), `git rm --cached`, every read-only verb, unknown verbs, and
anything on a clean tree or outside a repository. Unpushed commits are deliberately unprotected -- they are in the reflog. Only destructive
verbs are enumerated, so a new git subcommand does not arrive pre-blocked.

**Verified against a real Claude Code instance, not only unit tests** (`claude -p --plugin-dir plugins/no-work-loss --allowedTools Bash`,
2.1.233): on a dirty repo `git reset --hard` came back "The command was blocked by a safety hook", the reason reached the model verbatim, it
relayed the suggested `git stash push -u` rewrite, and the uncommitted edit survived. The negative control in the same repo -- `git checkout
-b feature-x` on the same dirty tree -- created the branch and carried the changes across. Note `--permission-mode bypassPermissions` is
rejected under root, which silently produces a run that proves nothing; use `--allowedTools Bash`.

Cost: prefilter is a substring scan for `git`/`rm`/`mv`/`>`/`tee`/`truncate` and returns before any parse or subprocess -- 64.8 ns, zero
allocations on a miss; ~1.8 ms end to end including process spawn, ~5.5 ms for a full deny with two git subprocesses. Repo state is probed
once per directory per invocation, and `--ignored` / `stash list` only for the verbs needing them. `status --porcelain` runs with
`--untracked-files=all`, without which git collapses an untracked directory to its name and `rm internal/config/env.go` finds no matching
entry and reads as safe.

- **Entry + fail-safety**: `plugins/no-work-loss/main.go` -- stdin JSON, the prefilter gate, the recover-into-deny, the deny payload
- **Prefilter**: `plugins/no-work-loss/prefilter.go` -- the cheap needle scan, and the destructive-verb markers used once parsing has failed
- **Segmentation**: `plugins/no-work-loss/segment.go` -- AST walk, cwd tracking, wrapper stripping, word staticness
- **Git verbs**: `plugins/no-work-loss/gitverb.go` -- global-option skipping, flag unbundling, per-verb hazard classification and rewrites
- **Non-git deletion**: `plugins/no-work-loss/fsverb.go` -- `rm`, `mv` destinations, `tee`, `truncate -s 0`, truncating redirects
- **Repository state**: `plugins/no-work-loss/repo.go` -- probing with timeouts and caching, `-z` status parsing, path containment
- **Decision**: `plugins/no-work-loss/decide.go` -- orchestration, the judge, and the denial text
- **Aliases**: `plugins/no-work-loss/alias.go` -- alias table, `!shell` expansion, the builtin-verb skip list
- **Reachability**: `plugins/no-work-loss/reach.go` -- containment, fast-forward, pushed-ness, reflog orphans, worktree probing
- **Tests**: `guard_test.go` (the deny/allow matrix against real repos), `refverbs_test.go` (unconditional denies + their safe spellings),
  `shell_test.go` (compound forms, wrappers, cd scoping), `hookio_test.go` (the raw JSON keys of the deny payload)
- **Depth**: `plugins/no-work-loss/docs/decision-model.md`

## The provenance half: every change to file content goes through Write, Edit or NotebookEdit

The plugin answers a second question of the same parsed command: not "would this destroy something" but "did this content get here through an
edit tool". Bash exists to run things -- git, builds, tests, validation, search -- and does not author files. The two halves share the shell walk,
the wrapper stripping and the path resolution, which is the whole reason they are one plugin: a second copy of that machinery would drift from
this one within a release.

**Where the two disagree, the destruction half's message wins**, because losing unsaved edits is the more urgent fact and its message names the
stash that saves them. Its suggested rewrites had to change for the merge: `> file` used to advise `>> file` and `tee` used to advise `tee -a`,
and both of those are now refused by the other half -- advice that gets denied on the next call is worse than no advice.

**This half fails CLOSED, the opposite of its sibling.** A command that does not parse, a target built from an expansion, a `cd` into a directory
the text does not name, a `GIT_DIR` that relocates the repository, an in-place `sed` whose files arrive from `xargs`: each denies, because a path
this hook cannot name is a path it cannot clear. A panic denies too. The destruction half keeps its fail-open posture (a bug while checking `ls`
must not wedge a session), so the two postures sit side by side deliberately -- one refuses what it cannot verify, the other only refuses what it
can see is dangerous.

**There is no opt-out.** No environment variable, no flag, no settings key. An opt-out is the hole.

**Stdin is a program only when nothing else named one.** `echo 'x' | ruby` hands ruby a script the command text does not contain, and denies. `printf
'{...}' | node hook.ts` hands a NAMED script its input, and the program is right there in the argv -- that is how a hook gets tested with the payload
it will really receive, and refusing it stopped ordinary work rather than any write. `namesAScript` is the difference. Closing that also closed the
matching hole in the other direction: `stdinScript` counted pipes and heredocs but not `<`, so a script piped in denied while the identical script
redirected in did not. `ruby < prog.rb` now denies too, and `node hook.ts < payload.json` stays allowed, because the operand check runs either way.
The stdin markers are untouched: `cat evil.js | node -` and `node /dev/stdin` still deny.

**Scope is paths, never commands.** A build directory (`build`, `dist`, `target`, `node_modules`, `.cache`, ...) is writable by whatever writes it;
anything outside the guarded roots, including the `/tmp` scratchpad, was never in scope. The guarded roots are the repository containing the
payload's `cwd` and `CLAUDE_PROJECT_DIR`, found by walking up for `.git` rather than paying for a subprocess in front of every Bash call.

**What it does NOT do, stated so nobody has to rediscover it**: it does not sandbox the programs it starts. `go build`, `npm test` and `make`
write what they write. What it closes is every route where the command text itself performs or directs the write. The formatters it vouches for
(`gofmt`, `goimports`, `shfmt`, `prettier`, `rustfmt`, `cargo fmt`, `terraform fmt`, `go generate`, `go-toolchain`) are an explicit table, not an
omission -- each writes only a canonical reformat or a repo-owned regeneration of the file it is handed. A tool NOT on that table does not become
allowed by being a formatter: an unrecognised in-place rewrite denies, and the way to run one is a named recipe (`just fmt`, `make fmt`).

- [docs/write-routes.md](docs/write-routes.md) -- every route, the shape of each rule, the two places the enumerated list collides with ordinary
  workflow, and the boundaries this half deliberately does not cross.
- **Route catalog**: `routes.go` (redirects, the argv writers, the unknown-tool in-place rule), `textproc.go` (sed's `w` and awk's `print >`,
  read out of the program text so a filter is not mistaken for a writer), `interpreters.go` (inline scripts, editors, the formatter table),
  `gitroutes.go` (git as an editor, and the plumbing that skips the worktree), `remote.go` (the GitHub API commit), `auditedroutes.go` (the five
  routes found by reading the permission rules rather than listing writers from memory), `tree.go` (guarded roots and the writable directories),
  `writeroutes.go` (the verdict, plus the tools that are not Bash at all).
- **Tests**: `routes_test.go` is a table of one deny and one control per route -- the same command aimed outside the tree, which is what makes each
  case load-bearing rather than a rule matching on shape. Every denial must NAME the path or route it stopped, so a deny arriving from an unrelated
  rule cannot satisfy the assertion. `merged_test.go` pins the cases where the two halves disagree; `tools_test.go` covers the non-Bash routes.

## The Write tool's own refusals (formerly the no-overwrites plugin)

`writetool.go` carries what used to be a separate bash hook. Write authors a whole file, so aimed at a path that already holds something it
replaces content nobody reviewed the loss of -- Edit is the tool for that. Once that is refused, "delete the path, then Write it" is the obvious
way round, so a path sitting in the recycle bin is refused too, naming `recycler restore <path>`: in that window the only other copy of the file is
in the model's context, where a compaction destroys it. It keeps no ledger of its own (recycler already tracks original locations) and compares
both the literal and the physically resolved path, because recycler records `/tmp/x` as `/private/tmp/x` on macOS. Every failure -- no recycler, an
unreadable bin, no match -- falls through to allow.

Relationship to the siblings: **`cleanup-bash-cmds`** rewrites
`rm` to `recycler trash`, which does NOT make `rm` safe on its own: hooks receive the original input so neither plugin sees the other's
rewrite, and `recycler` may be absent (it is missing in the web-session image, where the rewritten command simply fails). This plugin
evaluates `rm` as written; where both fire, the deny wins.
