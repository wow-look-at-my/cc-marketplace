# slh-workers

Lets Claude plan, coordinate and review while [slh](https://github.com/wow-look-at-my/simple-llm-harness) workers on a fast model write the code.

```bash
claude plugin install slh-workers
```

The plugin adds the MCP server `slh` with `start_task`, `wait_task`, `list_tasks` and `cancel_task`, plus the `delegate` skill that teaches the coordinator loop.

## Setup

- **A buildhost read token.** slh downloads from buildhost on first use. Export `BUILDHOST_READ_TOKEN` before you start Claude Code.
- **An slh config with a fast model.** Workers run on the `main` role of `$XDG_CONFIG_HOME/slh/config.xml`, or of the file `SLH_WORKERS_CONFIG` names. Until that exists, every `start_task` fails and says why.

Workers answer every permission prompt Allow. Give them a directory you are willing to let them change, and review the diff before you commit it.
