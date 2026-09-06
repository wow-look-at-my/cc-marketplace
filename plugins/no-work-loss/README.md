# no-work-loss

Keeps you in charge of the working tree. Two rules, one parse of the command:

- **Destruction** — a command that would destroy content existing only in the working tree. Committed history survives in the reflog. A modified or untracked file does not survive anything. So the hook commits that content into a dedicated ref, pushes it to origin, and then allows the command.
- **Provenance** — a change to file content that skips Write, Edit or NotebookEdit. Bash runs things. It does not author files. This one still refuses.

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install no-work-loss
```

## What it preserves, then allows

The content is committed to `refs/no-work-loss/<timestamp>`, pushed to origin, and the command proceeds. A push failure still allows — the local ref already holds it:

| Command | Preserved when |
|---|---|
| `git checkout <ref>` / `git switch <branch>` (no pathspec) | tracked files are modified |
| `git clean -fd` (`-fdx` adds ignored files) | untracked files exist |
| `git merge` / `git pull` | the tree is dirty |
| `rm`, `git rm`, `mv` (within the tree), `git submodule --force` | the target is modified or untracked |

```
preserved: rm would have lost 1 untracked file (scratch.txt), so it was committed to
refs/no-work-loss/20260906T153012.000000001 (a1b2c3d4e5f6) and pushed to origin
before being allowed to proceed.
```

## What it still refuses

Some commands are also provenance routes: `git reset --hard`, `git restore`, `git checkout -- <path>`, `git rebase`, `git cherry-pick`, `git revert`, `git am`, `tee`, `truncate -s 0`, `> file`. Each refuses whatever preservation did, because writing tracked content from Bash is authored change no edit tool made. `git stash drop`/`clear` also refuses: a stash entry is not preserved.

Ref-destroying commands are judged on whether the commits survive somewhere else, not on the verb:

| Command | Refused when |
|---|---|
| `branch -D` / `-M`, `update-ref -d` | no other ref contains the tip |
| `push --force` / `+refspec` | the remote tip is neither an ancestor of what you push nor held by another ref |
| `push --delete` | no other ref contains the remote tip |
| `filter-branch` | HEAD is not fully pushed |
| `reflog expire` / `delete` | a commit is reachable only through the reflog |
| `worktree remove --force` | that worktree has uncommitted changes |

`push --mirror` is refused outright: it rewrites every ref at once, so there is no bounded set of commits to check. A ref under `refs/no-work-loss/` is refused the other way — always, never conditionally — since it is the only copy of what it holds.

A denial that is not preserved names the counts, the files, and the command to run instead:

```
blocked: git reset --hard writes into /repo, which is inside the working tree.
Use Edit to change an existing file, or Write to create one.
```

## What it routes back to the edit tools

A change to file content under the working tree goes through Write, Edit or NotebookEdit. Refused whatever the repository's state. The message names the path it stopped:

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

Write itself is refused on a path that already exists (use Edit), and on a path sitting in the recycle bin. There `recycler restore` gets the bytes back, instead of re-authoring them from context.

## What it does not block

`git checkout -b`, `git switch -c`, `git stash push`, `git commit`, `git add`, `git restore --staged`, `git rm --cached`, every read-only verb, and any command on a clean tree. Builds, tests and generators write what they write — this is not a sandbox. Build output (`build/`, `dist/`, `node_modules/`, `.cache/`, ...) and everything outside the working tree, including `/tmp`, are writable by anything. The formatters that rewrite by design — `gofmt -w`, `prettier --write`, `go generate`, `go-toolchain` and friends — are an explicit allow list. Unpushed commits are not protected either. They are in the reflog. A guard that blocks ordinary work gets switched off, and then it protects nothing.

There is no opt-out: no environment variable, no flag, no settings key.

## Requirements

`git` on `PATH`. `recycler` for the recycle-bin check, which falls through to allow without it. No configuration.

## Notes

- Hazard model, reachability, the parser, ambiguity handling, and the one fail-safety gap the platform imposes: [docs/decision-model.md](docs/decision-model.md).
- Every write route, the formatter decision, and the boundaries: [docs/write-routes.md](docs/write-routes.md).
