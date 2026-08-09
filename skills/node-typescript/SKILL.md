---
description: Read before deciding whether a file has to be JavaScript, before adding a build step or a hand-written .d.ts to ship TypeScript, or when a .ts import fails to resolve at runtime. Corrects the stale belief that Node cannot run TypeScript - it has since v22.18/v23.6, and the resolution rules that make it work are not the ones I guess.
---

# Node runs TypeScript: what is actually true

Notes to self. Checked against `nodejs.org/api/typescript.html` and executed on Node
v22.22.2. Where my instincts and the docs disagree, **the docs win**.

## The wrong instinct this replaces

I reach for "Node can't run TypeScript, so this file has to be `.js`" and then build
something worse on top of that premise: a checked-in compiled `.js` plus a
hand-written `.d.ts`, or a build step, or `ts-node`. Then I *justify* it — "an
extensionless specifier can't resolve a `.ts`, so it must be JavaScript" — which is a
true fact about resolution turned into a false conclusion about the language.

**Node has run TypeScript by stripping types since v22.18.0 / v23.6.0, on by default,
and it is stable as of v24.12.0 / v25.2.0.** No flag, no loader, no build step. Check
`node --version` before concluding anything about what a file has to be.

Real cost of getting this wrong: an 81-line hand-written `.d.ts` shipped beside a `.js`
module, duplicating types that nothing verified against the implementation — deleted
the moment the file simply became `.ts`.

## Type stripping in one paragraph

Node erases the types and runs what is left. It does not *compile* — no transform, no
emit, no `tsconfig.json` consulted at runtime. So anything that needs codegen is
rejected rather than handled, and everything that is pure erasure just works.

- Default on since v22.18.0 / v23.6.0; stable since v24.12.0 / v25.2.0. Disable with
  `--no-strip-types`.
- `--experimental-strip-types` and `--experimental-transform-types` are the historical
  flags; `--experimental-transform-types` was **removed in v26.0.0**. Do not write
  either into a script or a CI step — on a current Node they are noise at best.
- Source maps are on, so stack traces point at the `.ts`.

## Extensions are mandatory — this is the part I get wrong

The rule that made me conclude "it has to be `.js`":

> "As in JavaScript files, file extensions are mandatory in `import` statements and
> `import()` expressions: `import './file.ts'`, not `import './file'`. Because of
> backward compatibility, file extensions are also mandatory in `require()` calls:
> `require('./file.ts')`, not `require('./file')`."

Verified: `require('/abs/mod.ts')` → works. `require('/abs/mod')` → `Cannot find
module`. Node never adds `.ts` to the extension search list.

Two more that follow from it, and both have bitten:

- **A `.js` specifier does not resolve to a `.ts` file.** `import './mod.js'` when only
  `mod.ts` exists is `ERR_MODULE_NOT_FOUND`. That is the TypeScript-compiler
  convention, not Node's — it applies when you *emit*, not when Node runs the source.
- **Type stripping does not transform module syntax.** A `.cts` file is CommonJS: ESM
  `export const` in it is a plain `SyntaxError: Unexpected token 'export'`. `.mts` is
  ESM. `.ts` follows the nearest `package.json` `"type"` as always.

## Getting types when you cannot set compiler options

If you control `tsconfig.json`, the documented answer is `allowImportingTsExtensions`
so `import { x } from './mod.ts'` type-checks. Its constraint (verified: `TS5096`) is
that it needs `noEmit` or `emitDeclarationOnly` — or `rewriteRelativeImportExtensions`,
which rewrites `./mod.ts` to `./mod.js` on emit, i.e. for compile-and-ship, not
run-directly.

When the compiler options are fixed by a host that will not let you add either — a
GitHub Action that type-checks inline scripts, an embedded tsc, a sandbox — a `.ts`
specifier is rejected (`TS5097`) and you may think you are forced back to `.js`. You
are not. Split the two resolutions, because they have different rules:

```ts
const { thing } = require("/abs/path/mod.ts") as typeof import("/abs/path/mod");
```

`require` takes the explicit `.ts`, which is what Node demands. The `typeof import(...)`
cast is a **type position**, where tsc resolves `mod.ts` from an extensionless
specifier and applies no extension rule. One line, fully typed from the real source, no
`.d.ts`, no build step. Verified under both `moduleResolution: node10` and `nodenext`,
including the negative control: a wrong argument still errors (`TS2353`), so this is
real type-checking and not an `any` in disguise.

## What is rejected, and how it fails

Erasable syntax only. Anything needing runtime codegen throws
`ERR_UNSUPPORTED_TYPESCRIPT_SYNTAX` at parse time (verified, each of these):

- `enum` — "TypeScript enum is not supported in strip-only mode". Use a union type or
  `as const` object.
- `namespace` with runtime code — same error. A **type-only** namespace is fine, since
  it erases to nothing.
- Parameter properties (`constructor(private x: number)`) and import aliases.
- Decorators are a TC39 Stage 3 proposal, not stripped: parser error.

Set `erasableSyntaxOnly: true` in tsconfig so tsc rejects these at author time instead
of letting Node be the one to find them.

**`import type` is mandatory for type-only imports.** Stripping is syntactic — Node
cannot know a named import is a type, so it leaves it as a value import and you get
`SyntaxError: The requested module './mod.ts' does not provide an export named
'Thing'` (verified). `verbatimModuleSyntax: true` makes tsc enforce the `type` keyword.
`import { fn, type FnParams } from './fn.ts'` is the mixed form.

**`node_modules` is refused outright**: `ERR_UNSUPPORTED_NODE_MODULES_TYPE_STRIPPING`,
deliberately, to discourage publishing TypeScript sources as packages. Type stripping
is for *your* source, not your dependencies — a published package still ships JS.

## The tsconfig Node's own docs recommend

```json
{
  "compilerOptions": {
    "noEmit": true,
    "target": "esnext",
    "module": "nodenext",
    "rewriteRelativeImportExtensions": true,
    "erasableSyntaxOnly": true,
    "verbatimModuleSyntax": true
  }
}
```

`noEmit` because Node runs the source; the last three make tsc enforce at author time
exactly what the runtime enforces.

## Decision, short form

Before writing a `.js` file or adding a build step to ship TypeScript: is this Node
≥ 22.18? Is the syntax erasable? Then write `.ts`, import it with its extension, and
stop. A checked-in compiled artifact or a hand-written `.d.ts` needs a reason that
survives those two questions — "Node can't run TypeScript" is not one.
