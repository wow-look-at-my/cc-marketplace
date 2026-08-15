# no-work-loss

Blocks Bash commands that would destroy uncommitted work. Committed history survives in the reflog; a modified or
untracked file does not survive anything.

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install no-work-loss
```

## What it blocks

Refused when the repository actually has something to lose:

| Command | Refused when |
|---|---|
| `git reset --hard` / `--merge` / `--keep` | tracked files are modified |
| `git checkout <ref>` / `git switch <branch>` | tracked files are modified |
| `git checkout -- <path>` / `git restore` | tracked files are modified |
| `git clean -fd` (`-fdx` adds ignored files) | untracked files exist |
| `git stash drop` / `git stash clear` | the stash is non-empty |
| `git rebase` / `merge` / `cherry-pick` / `revert` | the tree is dirty |
| `rm`, `mv`, `tee`, `truncate -s 0`, `> file` | the target is modified or untracked |

Ref-destroying commands are judged on whether the commits survive somewhere else, not on the verb:

| Command | Refused when |
|---|---|
| `branch -D` / `-M`, `update-ref -d` | no other ref contains the tip |
| `push --force` / `+refspec` | the remote tip is neither an ancestor of what you push nor held by another ref |
| `push --delete` | no other ref contains the remote tip |
| `filter-branch` | HEAD is not fully pushed |
| `reflog expire` / `delete` | a commit is reachable only through the reflog |
| `worktree remove --force` | that worktree has uncommitted changes |

So deleting a merged branch, force-pushing when the old tip is still on another branch, and dropping a remote branch
already merged to master are all allowed. `push --mirror` is the one exception refused outright: it rewrites every ref
at once, so there is no bounded set of commits to check.

Every denial names the counts, the files, and the command to run instead:

```
blocked: git reset --hard would lose 3 modified + 1 untracked file (src/a.go, src/b.go, +2 more).
run: git stash push -u -m "pre-reset" && git reset --hard origin/master
```

## What it does not block

`git checkout -b`, `git switch -c`, `git stash push`, `git commit`, `git add`, `git restore --staged`, appends
(`>>`, `tee -a`), `git rm --cached`, every read-only verb, and any command on a clean tree. Unpushed commits are
not protected either -- they are in the reflog. A guard that blocks ordinary work gets switched off, and then it
protects nothing.

## Requirements

`git` on `PATH`. No configuration.

## Notes

Depth on the hazard model, the parser, ambiguity handling, and the one fail-safety gap the platform imposes:
[docs/decision-model.md](docs/decision-model.md).
