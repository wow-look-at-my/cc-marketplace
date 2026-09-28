import { serve } from "./lsp.ts";
import { runHook } from "./hook.ts";

// One bundle, two entry points. The server reports every check while a file is
// open; the hook refuses the writes whose repair is mechanical. Both read
// checks.ts, so neither can drift from the other or from CI.
if (process.argv.includes("--hook")) {
  void runHook(process.stdin);
} else {
  serve(process.stdin, (chunk) => process.stdout.write(chunk));
}
