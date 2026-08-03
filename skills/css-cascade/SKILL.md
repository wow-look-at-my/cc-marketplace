---
description: Read before writing or editing any CSS -- stylesheet, styled-component, shadow-DOM style block, or a <style> in an HTML page. Corrects the reflex of giving every element its own complete rule, which produces the same declaration block written five times under five selectors. Covers what to put on the element vs the exception, inheritance, specificity math, :is/:where/:not, cascade layers, and how to hoist duplicates without breaking anything.
---

# CSS: use the cascade instead of repeating yourself

Notes to self. The wrong instinct is not a knowledge gap about syntax -- it is
reaching for "which selector do I add a rule for?" when the question is "what
should this ELEMENT look like, and what here is genuinely an exception?".

## The failure this exists to stop

Real file (webhook-runner's dashboard.css). No `a` rule anywhere. Instead, the
same block under five different selectors -- `#setup-section a:not(.btn)`,
`.crumbs a`, `a.gh-slug`, `a.kv-jump`, `.attention-banner a`:

```css
<selector>       { color: var(--accent); text-decoration: none; }
<selector>:hover { text-decoration: underline; }
```

Asked to style one more link, I read that file, saw the pattern five times,
and wrote a SIXTH copy. Correct fix was three lines total:

```css
a { color: var(--accent); text-decoration: none; }
a:hover { text-decoration: underline; }
```

...then deleting the five copies, leaving only the genuine exceptions (a link
that is a button, a nav entry, a chip that must read as body text). The file
got shorter, and the next link added needs no CSS at all.

**The tell, every time: I am about to type a declaration block that already
exists verbatim somewhere in this file.** Stop there. That is not a new rule,
that is a base rule I failed to write the first time.

## Order of attack when something needs styling

1. **Would this be right for every one of these elements on the page?** Put it
   on the type selector (`a`, `table`, `code`, `input`). No class, no rule per
   instance.
2. **Is it right for everything inside one container?** Put it on the
   container and let it inherit, or scope it (`.panel a`).
3. **Is it genuinely one thing behaving differently?** NOW write the specific
   rule, and put only the DELTA in it -- not a fresh copy of the base with one
   line changed.
4. **Is the same value appearing twice?** That is a custom property
   (`--accent` in `:root`), not two literals that will drift.

Before adding a declaration to an existing stylesheet, grep it:
`grep -n 'text-decoration' foo.css`. One command, and it is exactly what the
six-copy failure above skipped.

## Inheritance -- the half of the cascade that gets ignored

Verified against MDN (`CSS_cascade/Inheritance`). Inherited by default:
`color`, `font-family`/`font-size`/`font-weight`/`font-style`, `line-height`,
`letter-spacing`, `text-align`, `text-transform`, `visibility`. NOT inherited:
`background`, `border`, `margin`, `padding`, `width`/`height`, `display`.

- So `color` and the font stack belong on a container, once -- never repeated
  onto each descendant.
- `color: inherit` is the OPT-OUT, and it works on non-inherited properties
  too ("Works on both inherited and non-inherited properties"). This is how a
  link that must read as body text stops being accent-colored, without
  removing the base rule everything else relies on.
- `unset` = inherit for inherited properties, `initial` for the rest.
  `revert` rolls back to the user-agent (or user) stylesheet, not to nothing.
  `revert-layer` does the same within cascade layers. `all: revert` resets
  every property at once -- a sledgehammer for a third-party subtree, not a
  tool for ordinary rules.

## Specificity, exactly (MDN `CSS_cascade/Specificity`)

Three columns, compared left to right: **ID (1-0-0)**, then **CLASS** —
classes, attribute selectors, and pseudo-classes like `:hover` (0-1-0 each) —
then **TYPE** — type selectors and pseudo-elements like `::before` (0-0-1
each). The universal selector `*` and all combinators (`>`, `+`, `~`,
descendant space) add **nothing**. Inline `style` effectively outranks any
selector; only `!important` beats it.

Consequences worth internalizing:

- `a.gh-slug` (0-1-1) beats `a` (0-0-1), so a base type rule never fights an
  existing class rule -- **adding a base rule is safe by construction** for
  every element that already has one. What it changes is the elements that
  had NO rule (that is the point), so scan for bare instances of that element
  before hoisting.
- `:is()`, `:not()` and `:has()` add no weight themselves, but take **the
  weight of their most specific argument**: `p:not(#fakeId)` is 1-0-1. So
  `:not(.btn)` quietly costs a class column -- often the reason a rule needs
  a bigger hammer next to it.
- `:where()` "always has its specificity replaced with zero, 0-0-0". This is
  the tool for a base rule that must be trivially overridable:
  `:where(a) { ... }` is beaten by literally any `a` rule anywhere, in any
  order. Use it for defaults a consumer is expected to override; use a plain
  type selector for the page's own baseline.
- `!important` "reverses the order of stylesheets" -- it is not a specificity
  bump, and reaching for it means the rule is in the wrong place. Fix the
  structure instead; an `!important` added to win an argument is the next
  session's mystery.

## Cascade layers, when a codebase already uses them (MDN `@layer`)

- Declare order up front: `@layer base, components, utilities;`. Later layers
  win, and "once the layer order has been established, specificity and order
  of appearance are ignored" -- a lower-specificity rule in `utilities` beats
  a higher-specificity one in `base`.
- **Unlayered styles beat ALL layered styles** for normal declarations:
  "Styles that are not defined in a layer always override styles declared in
  named and anonymous layers." So dropping a quick unlayered rule into a
  layered codebase silently outranks the whole system -- put it in a layer.
- For `!important` the order is reversed: important declarations in layers
  beat important unlayered ones.

## Hoisting duplicates safely (the actual edit)

1. Collect the identical blocks and their selectors.
2. Write the shared block on the element (or the nearest common ancestor).
3. Delete the copies. Keep only rules with a real delta.
4. **Check what else that selector now catches** -- the elements that
   previously had no rule. That is where a base rule shows up as an unwanted
   change, and it is the one thing to verify visually or in a screenshot.
5. Anything that must NOT take the base gets an explicit opt-out
   (`color: inherit`) plus a one-line comment saying why. An unexplained
   opt-out gets "cleaned up" by a later session and the bug comes back.

## The opposite failure -- do not overcorrect

A base rule that every specific rule then has to override is worse than the
duplication it replaced: it adds a line to every rule instead of removing
five. If most instances need an override, the base is wrong -- narrow it,
scope it to a container, or drop it. Same for `:root` custom properties named
after one component, and for utility classes that exist in one place.

Duplication that is NOT a defect: identical blocks in different `@media`
contexts, `from`/`to` in `@keyframes`, vendor-prefixed pairs, and a design
token deliberately restated in a theme override. The rule is about the same
block repeated in the SAME context.
