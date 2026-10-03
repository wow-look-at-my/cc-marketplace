## Bash Read Plugin

The bash-read plugin lives at `plugins/bash-read/`. It is the first plugin here built as a hooks module (`hooks/hooks.json` `modules`), not as command hooks. A command hook can deny a Bash call or rewrite its `command`. However, it cannot change the tool. A `tool.call` module hook can answer the call itself. As a result, it is the only transparent way to turn a Bash read into a Read.

The hook on `tool.call` `{ tool: 'Bash' }` maps one plain `cat`, `head`, `tail` or `sed -n 'A,Bp'` call onto Read's `file_path`, `offset` and `limit`, one Read for each file. It runs each real Read through `$.tool.call`, so permissions and Read hooks see the call. It returns the Read text as Bash's `{ stdout, stderr, interrupted }`, which core checks against Bash's output schema. Several files get a `==> path <==` header each. A command it cannot map exactly, or any Read that fails, goes to `next(e)` and runs as written.

The result carries a `context` note. It names the command that did not run and each Read call that answered it, and tells the model to use Read. The goal is that the model learns the tool, not just that it gets the lines. A tool result is one per call, so the Reads share a single Bash result. `sed` maps only for a single file, because it numbers lines across all its files as one stream.

- **The parser refuses all shell syntax.** A pipe, a redirect, an expansion, a glob, a `~` or a second statement changes what runs, so `words` returns null on any of them. A missed mapping costs nothing. A wrong one shows the model the wrong lines.
- **A tail count from the end reads the file first.** Read takes an offset from the start, so `tail -n N` reads the line total through `$.fs.read`. A failed read falls back to Bash.
- **The command hooks beneath never see a mapped call.** Core raises PreToolUse beneath every `tool.call` hook, so slopfix's `file_read` deny does not fire for a call this plugin answers. It still fires for every command left to Bash.
- **The model still sees a Bash tool_use.** The tool name in the transcript stays `Bash`. Only the result is Read's.

- **Module**: `plugins/bash-read/hooks/register.ts`
- **Parser**: `plugins/bash-read/hooks/parse.ts`
- **Tests**: `plugins/bash-read/hooks/register.test.ts`, run by `claude plugin test plugins/bash-read`. The release workflow runs it for any plugin with a `hooks/*.test.ts`.
