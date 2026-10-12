---
description: Read before you write code yourself for a task that has an implementation part. Hand the implementation to slh workers on a fast model (the start_task and wait_task tools of the slh MCP server), and keep the planning, coordination and review for yourself.
---

# Delegate implementation to slh workers

You are the planner, the coordinator and the reviewer. The workers type the code. A worker is an slh coding agent on a model much faster than you. It has full read, write and shell access in the directory you give it. It sees nothing of your conversation.

The tools are on the `slh` server of this plugin: `start_task`, `wait_task`, `list_tasks`, `cancel_task`. They defer behind ToolSearch, so load them with `select:mcp__plugin_slh-workers_slh__start_task,mcp__plugin_slh-workers_slh__wait_task,mcp__plugin_slh-workers_slh__list_tasks,mcp__plugin_slh-workers_slh__cancel_task`.

## The loop

1. **Plan first.** Read enough of the code to split the work into units. Each unit is a change one worker can finish and verify alone.
2. **Write each brief as a complete task.** Name the goal, the files and functions, the constraints of the repository, the exact build and test command, and what the report must say. A worker that has to guess a convention gets it wrong. Paste the rule into the brief.
3. **Start the independent units in one message.** `start_task` returns at once, so parallel units run in parallel. Give parallel units disjoint files. Workers that edit one file overwrite each other.
4. **Wait with `wait_task`.** A running task comes back as "running". Call it again. Do not do the worker's job while you wait.
5. **Review the work, not the report.** The report is the worker's claim. Read the diff. Run the build and the tests yourself. A green claim over a red build is the common failure.
6. **Send fixes back with `continue_task`.** Name the defect, the file and line, and the expected result. The worker keeps its whole transcript, so the follow-up costs less than a new brief.
7. **Commit and push yourself**, after the review passes.

## Do the work yourself instead when

- the change is a few lines and the brief will be longer than the diff.
- the task is a design decision, a review, or a question about the code.
- `start_task` says no worker can start. The text names the reason. Report it to the user once, then do the work yourself.

## A good brief

```text
Repository: /home/user/myrepo (Go, built with `go-toolchain` from the root, never bare `go`).
Goal: add a --json flag to `myrepo list` that prints one JSON object per item.
Files: cmd/myrepo/list.go (the command), internal/items/item.go (the Item type).
Constraints: tests use testify; keep the text output byte-identical when --json is absent.
Verify: run `go-toolchain`; it must pass with no new warnings.
Report: the files you changed, the test you added, and the go-toolchain result line.
Do not commit.
```
