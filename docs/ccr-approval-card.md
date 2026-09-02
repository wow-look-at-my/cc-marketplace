# Why this plugin must NOT answer the Claude Code Remote approval card

`rules.xml` deliberately has no `<mcpServer name="Claude_Code_Remote">` block.
It used to, and that entry was the reason every `mcp__Claude_Code_Remote__*`
call failed. This file exists so nobody adds it back.

## The card

Calling any `mcp__Claude_Code_Remote__*` tool raises:

> **Allow Claude to use list environments (Claude Code Remote)?**
> This connector call requires your approval to proceed.
> `[Deny]  [Allow once]`

Every call. No "Allow always". Adding the tool to `permissions.allow` changes
nothing — that path is genuinely unreachable by local rules, and everything the
old version of this document said about *why* remains true:

1. Local rules evaluate normally, allow, and `tools/call` is dispatched.
2. The CCR MCP proxy answers with JSON-RPC `-32003` plus an `args_sha256` in
   `error.data` — "needs_approval".
3. The client raises a retroactive approval card whose precomputed decision
   (`behavior: "ask"`, `suppressAlwaysAllowRule: true`) is handed straight to
   `canUseTool`, so the whole deny/ask/allow-rule and classifier pipeline is
   never invoked and no approval can ever be persisted.

What was wrong was the conclusion drawn from step 3.

## The mistake: "a hook can answer it"

Because the precomputed decision is `ask` and not `allow`/`deny`, it falls
through to the ask path, where `PermissionRequest` hooks run concurrently with
the dialog. A local binary answers in milliseconds and beats a human every
time. That much is real — and it is precisely the problem.

The concurrency is not a race the hook can usefully win. In `L5p`, when
`awaitAutomatedChecksBeforeDialog` is falsy — the default for any top-level
tool call; it is only set true for sub-agent contexts — the dialog is **sent
first** and the hooks run alongside it:

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

`cancelRequest(g)` cancels the very bridge request that is displaying the card,
and `y?.()` tears down the listener that a human click would have resolved
through. That listener is the **only** code path in the bundle that produces a
decision with `source: { type: "user" }` for this flow.

Then the retry goes out:

```js
{ name: o, arguments: i, _meta: s }
```

Same `name`, same `arguments`, and a `_meta` carrying only
`claudecode/toolUseId`. **No approval token, no grant, no `args_sha256`
acknowledgement.** A hook-approved retry is byte-identical to the call the
server just rejected, so the server — which is waiting on a human interaction
it never received — rejects it again. The one permitted retry is already spent
(the `!S` guard), so the second `-32003` is rethrown to the model.

Telemetry names this exactly: the second failure emits
`fe("mcp_ccr_needs_approval", "retry_failed")`, never `"arm_not_fired"`.

## What the user sees

A card that appears and vanishes before it can be clicked, then
`MCP error -32003: MCP tool call requires approval`. That string is the
server's own JSON-RPC `message` echoed through `McpError` — not client text.

The instinct on seeing an instant error is "the card was never raised, so the
hook never got the chance". The opposite is true: the card was raised, the hook
got there first, and the hook is what removed it.

## The rule

**A `PermissionRequest` hook cannot make these calls succeed, and can only make
them fail.** The one working outcome is to leave the ask unanswered so the
dialog stands and a human clicks it. That is what an absent `<mcpServer>` block
achieves — the hook returns nothing for this server, `L5p`'s `if (!s)` branch
resolves to nothing, and the bridge listener stays alive until the real click
arrives.

This is not a limitation to work around. There is nothing to convey a
hook-sourced approval to the server; the source shows no field, header, or
side-channel that could.

## `send_later`, and why "but it needs to be unattended" is not a counterargument

`send_later` exists to schedule a message into a session nobody is watching, so
a per-call dialog does defeat its purpose. That was the original motivation for
the entry, and it is why the temptation to re-add it is strong.

It does not survive contact with the mechanism above: with the entry present
`send_later` does not work unattended, it does not work at all. A tool that
needs one click beats a tool that always fails. If unattended scheduling
matters, it needs a mechanism that does not route through this card — not an
allowlist entry that deletes the click.

## Nothing else automates it either — four dead ends, checked in the bundle

Removing the block restores the click. The obvious next question is whether the
click can be automated some other way. It cannot, and these are the four
attempts worth not repeating:

- **A local rule of any kind.** The retry hands `canUseTool` a pre-built
  decision as its sixth argument, and the wrapper opens
  `let u = c ?? (await p9t(...))`. With that argument present the rule engine is
  never called, so `permissions.allow`, `permissions.deny`, the auto-mode
  classifier, `dontAsk` and even `bypassPermissions` are not consulted. The
  decision is hardcoded `behavior: "ask"`, so it always reaches the dialog.
- **A `PermissionRequest` hook or an SDK `canUseTool` callback.** These are the
  same function in the same argument position, so they behave identically: the
  "allow" is local, it CANCELS the outstanding bridge request instead of
  completing it, and the retry that follows is byte-identical. `args_sha256`
  appears three times in the bundle and is only ever READ off the server's
  error — the client has no call that sends a grant back.
- **`--permission-prompt-tool`.** Setting `permissionPromptToolName` to
  anything but `"stdio"` fails the `!(Vhe() && Vhe() !== "stdio")` term that
  arms the retry, so no card appears, no retry happens, and the raw `-32003`
  is rethrown. It removes the recovery path rather than automating it.
- **Pre-approving the tools server-side.** `extra_allowed_tools` /
  `extraAllowedTools` have ZERO occurrences in the bundle; `allowedTools` is a
  purely local permission layer consumed by the same rule engine the retry
  skips. No session-ingress or ccr-sessions request body carries a permission
  or allowlist payload, so the client has no way to arm such a thing even if
  the server supports it.

The bridge round-trip a human's click completes is the only real network
exchange in this whole flow, and it is the only path that produces a decision
with `source: { type: "user" }`.

Worth knowing for the day this gets fixed upstream: a genuine
auto-resolve-by-polling-the-server mechanism already exists in the client
(`serverApprovalWatch`), and it resolves an ask WITHOUT transmitting anything
locally. It is wired to one unrelated feature and is simply not attached to the
connector decision object. That, not a hook, is the shape a real fix takes.

## What still works unattended, and what genuinely does not

The gate is on the TOOLS, not on the capabilities behind most of them, and the
difference decides whether a session is actually blocked:

- **Reading another org repo** -- `add_repo` prompts, but the session's git
  credential path is NOT limited to the declared Repository Scope, so a plain
  `git clone https://github.com/<org>/<repo>.git` just works with no prompt
  (verified). What is lost is registration: the clone's CLAUDE.md, skills and
  plugins are not loaded, because `register_repo_root` is gated too.
- **Watching a PR** -- use `mcp__github__subscribe_pr_activity`, which is on a
  different server and not gated.
- **Listing repos / environments** -- `gh repo list` and the like.
- **`send_later`** -- no substitute exists. This is the one capability the gate
  genuinely removes, and it is the one that mattered: a tool whose entire
  purpose is firing in a session nobody is watching cannot be used at all when
  every call needs a human. Use the `/loop` skill for a recurring cadence
  instead, and never arm a wake-up that assumes the call succeeded.

So the honest summary is not "CCR is broken" but "one CCR capability is gone
and the rest have unprompted equivalents".

## Upstream

`anthropics/claude-code#81362` (open) documents the `suppressAlwaysAllowRule`
literal and states that `permissions.allow` cannot affect this path. Related:
`#79711`, `#79983` open; `#61015`, `#61027`, `#61044`, `#61097`, `#61143`
closed while the behavior persists.

If it is ever fixed so the retry conveys the approval — or so local rules are
consulted — this becomes safe to revisit. Until then, re-adding the block
reintroduces the failure.
