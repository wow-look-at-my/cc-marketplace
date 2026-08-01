# Answering the Claude Code Remote approval card

The `Claude_Code_Remote` entry in `rules.xml` is the only `<mcpServer>` block
that is not a read-only allowlist, and the only one whose purpose is to **answer**
a permission prompt rather than to avoid raising one. This is why.

## The card

Calling any `mcp__Claude_Code_Remote__*` tool raises:

> **Allow Claude to use list environments (Claude Code Remote)?**
> This connector call requires your approval to proceed.
> `[Deny]  [Allow once]`

Every call. No "Allow always". Adding the tool to `permissions.allow` changes
nothing.

## Why the allowlist cannot help

The dialog is not raised by the local permission system. It is **retroactive** —
raised after the local rules already allowed the call and the request went out:

1. Local rules evaluate normally, allow, and `tools/call` is dispatched.
2. The CCR MCP proxy answers with JSON-RPC `-32003` plus an `args_sha256` in
   `error.data` — "needs_approval".
3. The client raises an approval card and, on allow, re-issues the *identical*
   call. An edited input aborts instead.

The retry calls `canUseTool` with a precomputed decision as its sixth argument:

```js
{
  behavior: "ask",
  suppressAlwaysAllowRule: true,
  message: `The ${fullyQualifiedName} connector requires approval for this call.`,
  decisionReason: { type: "other", reason: "This connector call requires your approval to proceed." }
}
```

`createCanUseTool` opens `let a = s ?? (await cM(...))`. With that argument
present, `cM` — the whole deny/ask/allow-rule and auto-mode-classifier pipeline —
is **never invoked**. The allow rule is not outranked; it is never read. And
`suppressAlwaysAllowRule` is hardcoded, with returned permissions filtered to
rules that already exist, so no approval on this path can ever persist one.

## Why a PermissionRequest hook CAN

The precomputed decision is `behavior: "ask"`, not `allow` or `deny` — so it does
**not** short-circuit. It falls through to the ask path, and that path races the
hook against the human:

```js
let y = QdE(t, i, l, n, c).then((H) => ({ source: "hook", decision: H }));
...
let R = await Promise.race([y, I]);
if (R.source === "hook") {
	if (R.decision) return (u.abort(), R.decision);
```

`QdE` runs the `PermissionRequest` hooks. When one returns allow, the dialog is
aborted and the retry proceeds — `if (D.behavior === "allow") { ... continue; }`.
The call succeeds with no human involved.

A local binary answers in milliseconds, so it wins the race against a dialog that
needs a person.

## Why it is PermissionRequest and not PreToolUse

This plugin registers on both events, and the split matters here:

- `PreToolUse` fires *before* dispatch. Nothing has asked for approval yet, so
  there is no card to answer. An allow there would also settle the call before
  the permission engine runs, overriding the user's own deny rules — which is why
  `denyOnly` suppresses every non-deny verdict on that path.
- `PermissionRequest` fires only once the decision is "ask", which is exactly the
  state the `-32003` retry puts the call into. It is the only event that can
  answer the card.

So the deny half of this plugin rides `PreToolUse` and the allow half rides
`PermissionRequest` — and this entry is the clearest illustration of why.

## What is in the set, and what is not

Included: `send_later`, `add_repo`, `register_repo_root`, `list_repos`,
`list_environments`, `list_triggers`, `subscribe_pr_activity`,
`unsubscribe_pr_activity`. These read state or adjust what a disposable session
can see. None of them mutates a repository.

Excluded and still prompting: `create_trigger`, `update_trigger`,
`delete_trigger`, `fire_trigger`. Those edit or delete Routines account-wide,
which is a different blast radius from "this session can now see one more repo".

`send_later` is the one that makes this non-optional rather than a convenience.
Its entire purpose is to schedule a message into a session nobody is watching; a
per-call dialog means the one tool built for unattended operation cannot be used
unattended.

## The failure mode this introduces

The hook is now load-bearing. If the binary is missing, errors, or returns
nothing, the race falls back to the human dialog — the same stall as before. That
is a degradation to the previous behavior, not to something worse, and it is
visible immediately (the prompt appears). The alternative considered was
`permissions.deny`, which unlists the tool entirely and is absolutely
stall-proof, but costs the capability outright.

## Upstream

`anthropics/claude-code#81362` (open) documents the same `suppressAlwaysAllowRule`
literal and states that `permissions.allow` cannot affect this path. Related:
`#79711`, `#79983` open; `#61015`, `#61027`, `#61044`, `#61097`, `#61143` closed
while the behavior persists. If it is ever fixed so the path consults local
rules, this entry becomes redundant rather than wrong.
