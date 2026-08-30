# The decision model

## The invariant

Content that exists only in the working tree -- modified but uncommitted, or untracked -- must never be lost by a command the agent runs.

Everything else is subordinate to that. Committed work is reachable from the reflog for weeks, so a command that only rewrites committed
history is not this plugin's problem. Working-tree content is in no git object at all: once it is overwritten there is nothing to recover
it from, no matter who is asking or how sure they are.

## The incident this was built for

    git checkout master             # a dirty tree silently rides along to master
    git reset --hard origin/master  # ...and the edits are gone, with no reflog entry

Note the shape. The first command is individually reasonable and destroys nothing; it is what makes the second one lethal. A guard that
only gated `reset --hard` would have watched this happen. So moving HEAD with a dirty tree is itself treated as a hazard, which is why
`git checkout <branch>` is refused on a dirty tree even though git would have "succeeded".

## Hazard classes, and why they are not one bit

The single most tempting simplification here is a boolean: is the tree dirty? It is wrong, and it is the failure that gets a guard
uninstalled. The verbs do not agree about what "dirty" means:

| Command          | tracked modifications | untracked files | ignored files | stash entries |
|------------------|-----------------------|-----------------|---------------|---------------|
| `reset --hard`   | destroys              | spares          | spares        | spares        |
| `clean -fd`      | spares                | destroys        | spares        | spares        |
| `clean -fdx`     | spares                | destroys        | destroys      | spares        |
| `stash drop`     | spares                | spares          | spares        | destroys      |
| `checkout <ref>` | destroys              | spares          | spares        | spares        |

A boolean makes `git clean -fd` refuse over an edited tracked file it will not touch, and makes `git reset --hard` refuse over a scratch
file it will not touch. Both are false positives on the safe half of a legitimate command, and both teach the user that the guard is noise.
So each verb is checked only against the classes it can actually reach. `TestResetHardSparesUntrackedFiles` and
`TestCleanSparesTrackedModifications` pin the two halves.

## Commands that destroy refs: reachability, not refusal

A separate family destroys refs, commits or the reflog rather than working-tree content: `push --force`, `push --delete`, a `+refspec`,
`branch -D`, `branch -M`, `reflog expire`, `reflog delete`, `update-ref -d`, `filter-branch`, `worktree remove --force`.

None of these is destructive on its own. Deleting a merged branch, force-pushing after a rebase whose old tip is still on another branch,
dropping a remote branch whose commits are already on master -- all of these lose nothing, and a guard that refuses them is a guard that gets
switched off. So the question asked is not "is this verb dangerous" but **does this content exist anywhere else**:

| Verb | What must survive elsewhere | How it is answered |
|---|---|---|
| `branch -D` / `-M` | the branch tip | another ref contains it |
| `push --force` / `+refspec` | the remote-tracking tip | it is an ancestor of what is being pushed, or another ref contains it |
| `push --delete` | the remote-tracking tip | another ref contains it |
| `update-ref -d` | the ref's tip | another ref contains it |
| `filter-branch` | all of HEAD | `rev-list --count HEAD --not --remotes` is 0 |
| `reflog expire` / `delete` | nothing reflog-only | `fsck --unreachable --no-reflogs` finds no commit |
| `worktree remove --force` | that worktree's edits | its `status --porcelain` is empty |

Two facts here were established by running git, and both had already produced a wrong answer in a draft:

- **`--exclude` does not take a full refname.** For `--branches` and `--remotes` the pattern matches the name *without* the `refs/heads/` or
  `refs/remotes/` prefix. `--exclude=refs/heads/feature --branches` silently excludes nothing, so a branch holding the only copy of a commit
  reported "0 would be lost". A silent false negative is the worst outcome available here, which is why containment via `for-each-ref
  --contains` is used instead of hand-built exclusion lists.
- **`refs/remotes/<remote>/HEAD` is a symbolic alias** for the branch being overwritten. Counting it as "somewhere else" made every force
  push look safe. It is filtered out explicitly.

`push --mirror` remains an unconditional refusal: it rewrites every ref at once, so there is no bounded set of commits whose survival could
be checked. It is the only member of the family without a reachability answer.

When a push cannot be verified -- no remote-tracking ref exists locally -- the answer is deny, not allow. Absence of a local mirror is not
evidence the remote is empty; the fix named in the denial is `git fetch`.

A force push judged safe is still judged against possibly-stale local knowledge of the remote, which is exactly what `--force-with-lease`
exists to close. The denial path recommends it; the allow path cannot, since by then there is nothing to warn about.

## What is deliberately NOT blocked

- **Unpushed commits.** `git reset --hard origin/master` on a clean tree is allowed even when HEAD is ahead of upstream. Those commits are
  in the reflog. Blocking here would refuse a routine, reversible operation and buy nothing.
- **Anything already pushed, merged, or living on another branch.** This is the whole point of the reachability section above: recoverable
  content is not protected content. Note that it cannot apply to the working tree -- a modified or untracked file's current bytes are in no
  commit by definition, so "already pushed" is never true of them. That is why the dirty-tree verbs stay state-based and the ref verbs are
  reachability-based. `git stash drop` sits with the former: stashing is precisely the act of putting content somewhere no branch points at.
- **`git checkout -b` / `git switch -c`.** Creating a branch carries changes across; it cannot drop them. Dirty or not, allowed.
- **`git stash push`, `git commit`, `git add`.** These create recovery. `stash push` is also the suggested fix in most denials, so blocking
  it would make the guard unescapable.
- **`git restore --staged`** without `--worktree`: it rewrites the index from HEAD and leaves the file on disk, so the content survives.
- **Read-only verbs, and unknown verbs.** Only destructive verbs are enumerated. Everything else falls through to allow, so `git status`
  costs one substring scan and a new git subcommand does not arrive pre-blocked.
- **Appending.** `>>`, `tee -a`, and `git rm --cached` all leave content in place.
- **Anything outside a git repository.** There is no uncommitted work to lose, and policing a user's whole filesystem is not this
  plugin's job.

## Detection

Substring matching on `git reset --hard` is not sufficient and gives false confidence. The command is parsed into an AST with
`mvdan.cc/sh/v3/syntax` -- the same parser the `enhanced-auto-allow` plugin uses -- and flattened into every unit that will execute:

- **Chains.** `&&`, `||`, `;`, `|`, newlines, brace blocks, subshells, `if`/`while`/`for`/`case` bodies, function bodies, and command
  substitutions. Each is evaluated on its own.
- **Working directory.** A `cd` is tracked across the sequence, so `cd /other/repo && git reset --hard` is checked against `/other/repo`.
  The cwd pointer is shared only where the shell shares one: `&&`, `||`, `;` and brace blocks carry a `cd` forward; a pipe stage, a
  subshell and a conditional body each get a copy. `git -C <path>` and `--work-tree` are applied the same way.
- **Wrappers.** `env`, `sudo`, `doas`, `command`, `builtin`, `exec`, `nohup`, `nice`, `ionice`, `setsid`, `stdbuf`, `timeout`, `xargs`, a
  leading `\`, and an absolute path all resolve to the same program. Value-taking flags are understood, because `nice -n 10 git reset
  --hard` otherwise leaves `10` sitting where the program should be and the git behind it is never seen -- a real bug this cost.
- **Flags.** Short flags are unbundled, so `-fdx`, `-xdf` and `-f -d -x` are the same command. `--flag=value` registers as `--flag`. `--`
  separates operands.
- **Aliases.** Resolved out of `git config --get-regexp '^alias\.'`, including the `!shell` form, which is re-parsed as shell. Builtin
  verbs skip the lookup entirely, since git refuses to let an alias shadow one. Chains resolve; self-reference terminates at depth 3.

## Ambiguity resolves to denial

Three cases produce a denial without a state answer, because a destructive verb whose target cannot be identified is exactly what this
plugin exists to refuse:

- The command does not parse **and** names a destructive verb. (Unparseable with nothing destructive in it is allowed.)
- An operand is not statically known -- `rm $TARGET`, `cd $DIR && git reset --hard`, `cd -`.
- `GIT_DIR` / `GIT_WORK_TREE` / `--git-dir` relocate the repository away from the path words, so the target is no longer knowable.

## Fail-safety, stated honestly

Inside the process, failure denies for destructive verbs: a panic is recovered and converted to a denial, a git subprocess that errors or
exceeds the 3-second timeout produces "cannot tell whether ... would lose uncommitted work", and an unreadable repository is never assumed
clean. Everything non-destructive fails open, so a bug here cannot brick a session.

**The one gap, which cannot be closed from inside the plugin:** Claude Code's own documentation states that a hook which times out does not
block the tool call -- "don't count on a stalled hook to act as a gate". So if the binary itself is killed or hangs past the harness
timeout, the command proceeds. The internal 3-second git timeout exists to keep the process well inside that window so the deny path is
reached rather than the harness's, but a hook cannot make itself mandatory. This is a property of the platform, not something the plugin
declines to handle.

## Cost

The prefilter is a substring scan for `git`, `rm`, `mv`, `>`, `tee`, `truncate`; anything else returns before a parse and before any
subprocess. Measured: 64.8 ns and zero allocations on a miss, ~1.8 ms end to end including process spawn, ~5.5 ms for a full denial with
two git subprocesses. Repository state is probed at most once per directory per invocation; `--ignored` and `stash list` are fetched only
for the verbs that need them.

## Interaction with the sibling plugins

- **The `Write` tool's own refusals** (a path that exists, a path in the recycle bin) live in `writetool.go`, and are strictly stronger than
  "blocks Write on a dirty file", so that half of the guarantee is not duplicated in the hazard model here. Shell truncation (`>`, `tee`,
  `truncate -s 0`) is a Bash concern and is handled by this model.
- **`cleanup-bash-cmds`** rewrites `rm` into `recycler trash`. It does not make `rm` safe on its own: hooks receive the original input, so
  neither plugin can see the other's rewrite, and `recycler` may not be installed -- on a machine without it the rewritten command simply
  fails. This plugin therefore evaluates `rm` as written. Where both fire, the deny wins, which is the correct precedence.
