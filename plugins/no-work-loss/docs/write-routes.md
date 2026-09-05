# Write routes: how the provenance half decides

The rule: any change to file content under the session's working tree goes through Write, Edit or NotebookEdit. This document is the depth behind the summary in `../CLAUDE.md`.

## The three shapes a write takes

Every route resolves to one of three, and the verdict follows from the shape rather than from the program's name:

| Shape | Meaning | Denies when |
|---|---|---|
| named path | the target is on the argv (`sed -i x.go`, `> x.go`, `cp a x.go`) | the path resolves inside a guarded root and is not under a build directory |
| whole directory | the write lands *somewhere* under a directory (`patch`, `tar -x`, `git apply`, `split`) | that directory contains a guarded root, or sits inside one |
| opaque | the target cannot be resolved at all (an inline `node -e`, an `xargs`-fed `sed -i`, a GitHub API commit) | always -- fail closed treats an unresolvable target as the worst one |

A path that is not statically known (`sed -i "$F"`). A directory that is not (`cd "$D" && ...`), and a repository relocated by `GIT_DIR` all collapse to the same answer: deny. This is the half of the plugin that refuses what it cannot verify.

## The routes

**In-place editors.** `sed -i` in every spelling (`-i.bak`, a cluster like `-ri`, `--in-place=`), plus sed's `w`/`W` commands and the `w` flag on an `s` command, read out of the script text. `awk -i inplace` and `awk '{print > "f"}'` -- awk's grammar makes an unparenthesised `>` after `print` a redirect and anything else a comparison, which is what keeps `awk '$1 > 5'` a filter and not a writer. `ed`, `ex`, `vi`, `vim`, `nvim`, `emacs` (a file operand is the target. A `--batch --eval` with no operand is opaque. `emacs --version` writes nothing and is not an editing session). `busybox` resolves to its applet, so `busybox sed -i` reaches the sed rule.

**Interpreters.** An inline script -- `node -e`, `ruby -e`, `perl -e`/`-E`, `python -c`, `php -r`, `lua -e`, `deno eval`, `Rscript -e`, `osascript -e` -- is opaque and denies wherever it runs, as does a script arriving on stdin through a pipe or a heredoc. `jq` is listed with an empty flag set on purpose. It has no way to write a file, and the entry exists so nobody adds one. A script *file* is judged by where it lives: shell scripts are read and analysed (see indirection below). A script inside the tree got there through Write or Edit and is visible in the diff.

The session scratchpad is the one temporary directory that does NOT deny. Claude Code's own system prompt tells a session to put every temp file there rather than in `/tmp`. `isSessionScratchpad` matches a `scratchpad` segment under an ancestor named `claude` or `claude-<something>`, inside a scratch root -- both halves required, so a bare `/tmp/scratchpad` or a stray `claude-notes/` is still refused.

**Redirection and copy-over.** `>`, `>>`, `>|`, `&>`, `<>`, and `>&file` (but not `2>&1`, which duplicates a descriptor, and not `/dev/null` and friends). `tee` and `tee -a`, `dd of=`, `truncate -s`, `sponge`, `xxd -r`, `base64`/`openssl -out`.

**cp, mv, install, rsync, scp** are the one family with a source-side test. It is where the rule's real shape shows. This is about content *entering* the tree. Bytes already in the tree have been through an edit tool, so `mv old.go new.go` is ordinary refactoring and passes. A source from outside -- including a remote `host:/path` -- is the splice this closes, and `mv` in particular is the documented way around a guard that only blocks Write on an existing path.

**Patch application.** `patch` (the files are named inside the diff. The write is a whole-directory one against `-d` or the working directory), `git apply`, `git apply --cached`, `git am`.

**git used as an editor.** `checkout` with a pathspec, `restore` (but not `--staged` alone, which moves the index and leaves the file), `stash pop`/`apply`, `revert`, `cherry-pick`, `merge`, `rebase`, `pull`, `reset --hard/--merge/--keep`. Branch navigation is untouched: `checkout -b`, `switch -c`, and a bare ref switch all pass, because a session is required to work on a branch. `--abort`, `--quit`, `--continue` and `--skip` pass as well -- the content decision was made when the operation started.

**The plumbing route**, which never touches the worktree: `hash-object -w`, `update-index`, `commit-tree`, `mktree`, `read-tree`, `checkout-index`, `fast-import`, and `update-ref` when it sets a ref. `update-ref -d` deletes and introduces no content. It stays with the destruction half, which already asks whether the commits it drops survive elsewhere.

**Extraction and download into the tree.** `tar -x` (against `-C` or the working directory), `tar -c -f` writing an archive into it, `unzip`, `curl -o`/`-O`/`--output-dir`/`--cookie-jar`/`--dump-header`, `wget -O`/`-P`, `gh release download`. A pipeline is judged stage by stage, so `curl ... | tar -xz` is caught at the tar.

**Indirection this hook follows** rather than gives up on: `sh -c '...'` and an alias definition are shell source and are parsed. A shell script file is read from disk and parsed (a script that does not exist writes nothing and is left alone, one that exists and cannot be read or parsed denies). `./script.sh` is followed when its shebang names a shell. `find -exec`/`-execdir` has its utility lifted out and walked as a call of its own (`-execdir` runs in a directory the text does not name, so its paths are unresolvable). `xargs`, `env`, `sudo`, `timeout`, `nice` and the rest are stripped with their value-taking flags understood. A function body is walked, and a background `&` changes nothing -- the writes it performs are the same writes.

**Symlinks.** `ln`/`ln -sf` is judged on the link name, because pointing a tracked path at writable storage changes what the tree holds with nothing else seeing it.

**Routes found by audit** rather than from memory, by reading the commands this environment's permission rules already allow: `sort -o`, `split`/`csplit`, the compressors that replace their input (`gzip`, `bzip2`, `xz`, `zstd`, ... unless `-c`), `zip`, `docker cp` out of a container, `scp` from a remote host, and `yq -i`.

**Writes that never touch a local file.** `gh api --method PUT .../contents/...`, the `createCommitOnBranch` GraphQL mutation, the same two through `curl`, and the `mcp__*__create_or_update_file` / `push_files` / `delete_file` family. The file content rides in the request and never exists on disk, so every path rule above misses them.

**Routes that are not Bash at all.** A subagent spawn (`Agent`, `Task`, `create_session`) carrying a tool grant or a permissive `permissionMode` is refused. A child must not be handed what the parent was denied, and a spawn asking for neither is ordinary delegation. The live settings (`~/.claude/settings.json`, any `.claude/settings*.json`) are refused to every tool including Edit, and so are the skills whose purpose is to rewrite them. A repository's own plugin sources are not this: editing `plugins/x/.claude-plugin/plugin.json` is ordinary work on source code.

## The unknown-tool rule, and why the formatter table is a decision rather than an omission

A catalog of program names leaks the moment a session reaches for one nobody named. So a long in-place flag -- `--in-place`, `--in-place=`, `--write` -- counts for any program at all, recognised or not. Short `-i` and `-w` are ambiguous (`grep -w`, `curl -w`) and count only for the tools known to spell in-place that way. Operands that are plainly not filenames are dropped from the report, so `yq -i '.a = 1' config.yaml` names the file rather than the program.

That leaves the tools which rewrite by design. They are an explicit table (`allowedFormatter`). The principle is stated there: each one writes only a canonical reformat, or a regeneration the repository owns, of the file it is handed. A tool not on the table is not allowed by being a formatter. `ffs fmt -w` is the worked example. It is recognised as an in-place rewriter and denied, and the way to run it is a named recipe, which is a reviewable line in the repository rather than an argv.

## Where this collides with ordinary workflow

Stated plainly rather than carved out, because a carve-out nobody sees is how a guard stops meaning anything:

- **`echo x > new-file` is refused**, even though nothing is lost. Creating a file with content in it is exactly what Write is for. The rule is consistent. It is also the refusal a session meets most often.

## `git merge` and `git pull` integrate. They do not author

Neither is in `worktreeVerbs`, so neither is refused. Every byte a merge writes is already in a commit, with a diff to read and a reflog to reach it by -- the same reasoning that lets a bare-ref `git checkout` through. Refusing them made the base-branch merge the PR rules require impossible. This half has no opt-out, and no edit tool performs a merge.

`rebase`, `cherry-pick`, `am` and `apply` are not the same act and stay refused. The first two replay commits onto a different base. The last two take a patch from outside git. What those land is not a tree anything already holds.

Integrating into a dirty tree is a separate question. The destruction half answers it: both verbs reach `hazTracked` there and are refused with the `git stash push -u` rewrite.

`integrate_test.go` pins all three halves of that: allowed on a clean tree, denied on a dirty one, and a patch still refused either way.

## What this half deliberately does not cover

- **Running programs.** A build, a test run, a generator or a `just` recipe writes what it writes. Sandboxing arbitrary execution is a different mechanism, and pretending otherwise will mean denying every build.
- **Deletion.** `rm`, `find -delete` and `git clean` remove content rather than author it. The destruction half and `cleanup-bash-cmds` own that.
- **File metadata.** `chmod`, `chown`, `touch` and `mkdir` change no content.
- **A hook that times out.** Claude Code's own docs state that a hook which times out does not block the call, so a killed binary lets the command through. A hook cannot make itself mandatory. This is platform behaviour, not an unhandled case.
