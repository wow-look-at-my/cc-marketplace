# Why this plugin must NOT answer the Claude Code Remote approval card

`rules.xml` deliberately has no `<mcpServer name="Claude_Code_Remote">` block. It used to. That entry was the reason every `mcp__Claude_Code_Remote__*` call failed. This file exists so nobody adds it back.

## The card

Calling any `mcp__Claude_Code_Remote__*` tool raises:

> **Allow Claude to use list environments (Claude Code Remote)?**
> This connector call requires your approval to proceed.
> `[Deny]  [Allow once]`

Every call. No "Allow always".

1. Local rules evaluate normally, allow, and `tools/call` is dispatched.
2. The CCR MCP proxy answers with JSON-RPC `-32003` plus an `args_sha256` in `error.data` — "needs_approval".
3. The client raises a retroactive approval card whose precomputed decision (`behavior: "ask"`, `suppressAlwaysAllowRule: true`) is handed straight to `canUseTool`.

What was wrong was the conclusion drawn from step 3.

## The mistake: "a hook can answer it"

A local binary answers in milliseconds and beats a human every time. That much is real — and it is precisely the problem.

The concurrency is not a race the hook can usefully win. In `L5p`, when `awaitAutomatedChecksBeforeDialog` is falsy — the default for any top-level tool call. It is only set true for sub-agent contexts — the dialog is **sent first** and the hooks run alongside it:

```js
f.sendRequest(g, t.tool.name, o, t.toolUseID, x, ...);   // the card appears
...
if (!s)
  (async () => {
    let x = await t.runHooks(...);
    ...
    if (f && g) f.cancelRequest(g);                       // the card is killed
    (y?.(), S?.(), d(x));
  })()
```

`cancelRequest(g)` cancels the very bridge request that is displaying the card, and `y?.()` tears down the listener that a human click will have resolved through. That listener is the **only** code path in the bundle that produces a decision with `source: { type: "user" }` for this flow.

Then the retry goes out:

```js
{ name: o, arguments: i, _meta: s }
```

The one permitted retry is already spent (the `!S` guard). The second `-32003` is rethrown to the model.

Telemetry names this exactly: the second failure emits `fe("mcp_ccr_needs_approval", "retry_failed")`, never `"arm_not_fired"`.

## What the user sees

A card that appears and vanishes before it can be clicked, then `MCP error -32003: MCP tool call requires approval`. That string is the server's own JSON-RPC `message` echoed through `McpError` — not client text.

The instinct on seeing an instant error is "the card was never raised, so the hook never got the chance". The opposite is true. The card was raised, the hook got there first. The hook is what removed it.

## The rule

That is what an absent `<mcpServer>` block achieves — the hook returns nothing for this server, `L5p`'s `if (!s)` branch resolves to nothing. The bridge listener stays alive until the real click arrives.

This is not a limitation to work around. There is nothing to convey a hook-sourced approval to the server. The source shows no field, header, or side-channel that can.

## `send_later`, and why "but it needs to be unattended" is not a counterargument

`send_later` exists to schedule a message into a session nobody is watching, so a per-call dialog does defeat its purpose. That was the original motivation for the entry. It is why the temptation to re-add it is strong.

It does not survive contact with the mechanism above: with the entry present `send_later` does not work unattended. It does not work at all. A tool that needs one click beats a tool that always fails. If unattended scheduling matters. It needs a mechanism that does not route through this card — not an allowlist entry that deletes the click.

## Nothing else automates it either — four dead ends, checked in the bundle

Removing the block restores the click. The obvious next question is whether the click can be automated some other way. It cannot. These are the four attempts worth not repeating:

- **A local rule of any kind.** The retry hands `canUseTool` a pre-built decision as its sixth argument, and the wrapper opens `let u = c ?? (await p9t(...))`. With that argument present the rule engine is never called, so `permissions.allow`, `permissions.deny`, the auto-mode classifier, `dontAsk` and even `bypassPermissions` are not consulted. The decision is hardcoded `behavior: "ask"`, so it always reaches the dialog.
- **A `PermissionRequest` hook or an SDK `canUseTool` callback.** These are the same function in the same argument position, so they behave identically. The "allow" is local. It CANCELS the outstanding bridge request instead of completing it, and the retry that follows is byte-identical. `args_sha256` appears three times in the bundle. It is only ever READ off the server's error. No client call sends a grant back.
- It removes the recovery path rather than automating it.
- **Pre-approving the tools server-side.** `extra_allowed_tools` / `extraAllowedTools` have ZERO occurrences in the bundle. `allowedTools` is a purely local permission layer consumed by the same rule engine the retry skips. No session-ingress or ccr-sessions request body carries a permission or allowlist payload. The client has no way to arm such a thing even if the server supports it.

The bridge round-trip a human's click completes is the only real network exchange in this whole flow. It is the only path that produces a decision with `source: { type: "user" }`.

Worth knowing for the day this gets fixed upstream: a genuine auto-resolve-by-polling-the-server mechanism already exists in the client (`serverApprovalWatch`). It is wired to one unrelated feature and is simply not attached to the connector decision object. That, not a hook, is the shape a real fix takes.

## What still works unattended, and what genuinely does not

The gate is on the TOOLS, not on the capabilities behind most of them, and the difference decides whether a session is actually blocked:

- What is lost is registration: the clone's CLAUDE.md, skills and plugins are not loaded, because `register_repo_root` is gated too.
- **Watching a PR** -- use `mcp__github__subscribe_pr_activity`, which is on a different server and not gated.
- **Listing repos / environments** -- `gh repo list` and the like.
- **`send_later`** -- no substitute exists. This is the one capability the gate genuinely removes. Use the `/loop` skill for a recurring cadence instead, and never arm a wake-up that assumes the call succeeded.

So the honest summary is not "CCR is broken" but "one CCR capability is gone and the rest have unprompted equivalents".

## Upstream

`anthropics/claude-code#81362` (open) documents the `suppressAlwaysAllowRule` literal and states that `permissions.allow` cannot affect this path. Related: `#79711`, `#79983` open. `#61015`, `#61027`, `#61044`, `#61097`, `#61143` closed while the behavior persists.

If it is ever fixed so the retry conveys the approval — or so local rules are consulted — this becomes safe to revisit. Until then, re-adding the block reintroduces the failure.
