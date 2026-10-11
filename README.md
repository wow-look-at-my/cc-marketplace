# rustfmt-enforce

Makes rustfmt impossible to skip.

- After every Write, Edit or MultiEdit of a `.rs` file, the hook runs rustfmt on that file and tells the model it changed.
- Before every `git commit`, the hook runs rustfmt on each staged or modified tracked `.rs` file. If rustfmt changed a file or rejected one, the commit is denied with the list.

The edition comes from the nearest `Cargo.toml`, the same way `cargo fmt` picks it. The project's `rustfmt.toml` and `rust-toolchain.toml` apply.

Requires `rustfmt` on `PATH` (`rustup component add rustfmt`). Without it, an edit to a `.rs` file reports the missing tool and a commit of Rust changes is denied.

## Install

```bash
claude plugin install rustfmt-enforce
```
