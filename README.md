# no-work-loss

Keeps you in charge of the working tree. Two refusals, one parse of the command:

- **Destruction** — a command that would destroy content existing only in the working tree. Committed history survives
  in the reflog; a modified or untracked file does not survive anything.
- **Provenance** — a change to file content that skips Write, Edit or NotebookEdit. Bash runs things; it does not
  author files.

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

## What it routes back to the edit tools

A change to file content under the working tree goes through Write, Edit or NotebookEdit. Refused whatever the
repository's state, and the message names the path it stopped:

| Route | Examples |
|---|---|
| in-place editors | `sed -i`, `awk -i inplace`, `ed`, `vim -c`, `perl -pi -e`, an inline `node -e` |
| redirection and copy-over | `> file`, `>> file`, `tee`, `dd of=`, `truncate`, `cp`/`mv`/`scp` from outside the tree |
| patch application | `patch`, `git apply`, `git apply --cached`, `git am` |
| git as an editor | `checkout <ref> -- path`, `restore`, `stash pop`, `revert`, `cherry-pick`, `merge`, `reset --hard` |
| git plumbing | `hash-object -w`, `update-index`, `commit-tree`, `checkout-index` |
| extraction and download | `tar -x`, `unzip`, `curl -o`, `wget -O`, `gh release download` |
| indirection | `bash ./script.sh`, `sh -c`, an alias, `find -exec sed -i`, `xargs sed -i` |
| server-side commits | `gh api PUT .../contents/...`, `createCommitOnBranch`, the GitHub MCP file tools |

```
blocked: sed -i writes builtins/math.ffs, which is inside the working tree.
Use Edit to change an existing file, or Write to create one.
```

Write itself is refused on a path that already exists (use Edit), and on a path sitting in the recycle bin, where
`recycler restore` gets the bytes back instead of re-authoring them from context.

## What it does not block

`git checkout -b`, `git switch -c`, `git stash push`, `git commit`, `git add`, `git restore --staged`, `git rm
--cached`, every read-only verb, and any command on a clean tree. Builds, tests and generators write what they write —
this is not a sandbox. Build output (`build/`, `dist/`, `node_modules/`, `.cache/`, ...) and everything outside the
working tree, including `/tmp`, are writable by anything. The formatters that rewrite by design — `gofmt -w`,
`prettier --write`, `go generate`, `go-toolchain` and friends — are an explicit allow list. Unpushed commits are not
protected either: they are in the reflog. A guard that blocks ordinary work gets switched off, and then it protects
nothing.

There is no opt-out: no environment variable, no flag, no settings key.

## Requirements

`git` on `PATH`. `recycler` for the recycle-bin check, which falls through to allow without it. No configuration.

## Notes

- Hazard model, reachability, the parser, ambiguity handling, and the one fail-safety gap the platform imposes:
  [docs/decision-model.md](docs/decision-model.md).
- Every write route, the formatter decision, and the boundaries: [docs/write-routes.md](docs/write-routes.md).
