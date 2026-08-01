# css-duplication

Surfaces the same declaration block written under more than one selector, as
**LSP diagnostics on the offending lines** while you edit the stylesheet.

The failure it catches is one specific bad habit: giving every element its own
complete rule instead of using the cascade, so `color: var(--accent);
text-decoration: none;` ends up under five selectors and the sixth link needs a
sixth copy. On the stylesheet that motivated the plugin it flags every copy,
with the other copies attached as related locations, and says what to write
instead.

## What it reports

A finding is a set of rules with an identical (normalized) body in the same
at-rule context:

- **two or more copies** of a body with two or more declarations, or
- **three or more copies** of a single-declaration body.

Findings are ranked by copies × declarations, so the strongest one comes first.
Not reported, because it is normal: duplication across different `@media` /
`@supports` contexts, `@keyframes` `from`/`to`, and vendor-prefixed pairs.

Preprocessor sources (`.scss`, `.less`) are deliberately ignored — nesting
changes what a repeated body means.

## How it runs

A language server, registered by `.lsp.json` and started automatically for
`.css` files. One warning per duplicated rule, anchored to the selector; the
first copy spells out the shared body and the other selectors carrying it, the
rest point back at it. Diagnostics refresh on open/change/save and clear
themselves the moment the block is hoisted, so nothing has to be dismissed.
Served both ways over stdio JSON-RPC — pushed via
`textDocument/publishDiagnostics` and pulled via `textDocument/diagnostic`.

Two things worth knowing before installing:

- Only one LSP server can claim `.css` — if another plugin already registers
  one, whichever loads first wins and the other never starts.
- The binary is built from this directory and launched from
  `${CLAUDE_PLUGIN_ROOT}`, so nothing needs to be on `PATH`.

## Installation

```bash
/plugin marketplace add wow-look-at-my/cc-marketplace
/plugin install css-duplication
```

The reasoning it is trying to install — specificity, inheritance, cascade
layers, and how to hoist a duplicate safely — lives in the `docs` plugin's
`/docs:css-cascade` skill.
