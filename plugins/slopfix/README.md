# slopfix

The org's slop guards. A language server reports, while you edit, the findings that fail the `common-checks` CI gate. Hooks judge each write, command and message.

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install slopfix
```

## Where the rules live

In [wow-look-at-my/slopfix](https://github.com/wow-look-at-my/slopfix). Every build fetches its latest binary from buildhost into `bin/slopfix.ape`, and the language server and every hook run it. A rule changed in slopfix ships with the next build.

Every diagnostic is an error, because every one of them fails the merge gate.
