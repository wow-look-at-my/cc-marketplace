# slopfix

The org's slop guards. A language server reports, while you edit, the findings that fail the `common-checks` CI gate. Hooks judge each write, command and message.

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install slopfix
```

## Where the rules live

In [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix). The plugin ships no copy of the binary. The language server and every hook run `scripts/slopfix`. It runs the slopfix on PATH when that one is at least the published version. Otherwise it downloads the published build and runs that. A rule changed in slopfix arrives as soon as buildhost publishes it.

Every diagnostic is an error, because every one of them fails the merge gate.
