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

## Commands refused without consulting state

A separate family destroys refs, commits or the reflog rather than working-tree content: `push --force`, `push --delete`, `push --mirror`,
a `+refspec`, `branch -D`, `branch -M`, `reflog expire`, `reflog delete`, `update-ref -d`, `filter-branch`, `worktree remove --force`.

These are refused unconditionally, because no local state could make them safe -- the remote history a force push overwrites is in nobody's
reflog but the author's. Each denial names the safe spelling instead (`--force-with-lease`, `branch -d`, `branch -m`), which is the whole
reason the deny is tolerable: the user's own command comes back with one flag swapped.

There is deliberately no `hazRefs` bit. It was written, went unused because every member of this family is an unconditional deny, and was
removed rather than left as decoration.

## What is deliberately NOT blocked

- **Unpushed commits.** `git reset --hard origin/master` on a clean tree is allowed even when HEAD is ahead of upstream. Those commits are
  in the reflog. Blocking here would refuse a routine, reversible operation and buy nothing.
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

- **`no-overwrites`** already blocks `Write` on any path that exists, which is strictly stronger than "blocks Write on a dirty file". That
  half of the guarantee is not duplicated here. Shell truncation (`>`, `tee`, `truncate -s 0`) is a Bash concern and is handled here.
- **`cleanup-bash-cmds`** rewrites `rm` into `recycler trash`. It does not make `rm` safe on its own: hooks receive the original input, so
  neither plugin can see the other's rewrite, and `recycler` may not be installed -- on a machine without it the rewritten command simply
  fails. This plugin therefore evaluates `rm` as written. Where both fire, the deny wins, which is the correct precedence.
