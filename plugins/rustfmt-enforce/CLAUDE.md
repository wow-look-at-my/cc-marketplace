## rustfmt-enforce Plugin

The rustfmt-enforce plugin lives at `plugins/rustfmt-enforce/`. It has one binary for multiple hooks.

- **PostToolUse (Write, Edit, MultiEdit)** formats the `.rs` file the tool wrote. A change goes back to the model as `additionalContext`. The next Edit reads the file again. A rustfmt failure exits 2 with rustfmt's stderr.
- **PreToolUse (Bash)** finds each `git commit` in the command (it follows `cd DIR` and `git -C DIR`). It formats every staged or modified tracked `.rs` file in that repository. A changed or rejected file denies the commit. The model then runs `git add` and commits again.
- **rustfmt reads the file on stdin.** A path argument makes rustfmt follow `mod` declarations and rewrite files nobody touched. The working directory is the file's directory, so `rustfmt.toml` and `rust-toolchain.toml` still apply.
- **The edition is `cargo fmt`'s**: the nearest `[package].edition`, through `edition.workspace = true` to `[workspace.package]`, else `2015` inside a package. Outside a package no `--edition` is passed.
- **A missing rustfmt fails loud.** It never reads as a clean file. `RUSTFMT_ENFORCE_BIN` points the hook at another binary, for tests.

- **Hook binary**: `main.go` (dispatch and output shapes), `format.go` (rustfmt and the edition), `commit.go` (the commit gate)
- **Tests**: `hook_test.go` runs the real rustfmt and real git repositories
