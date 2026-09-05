import { serve } from "./lsp.ts";

serve(process.stdin, (chunk) => process.stdout.write(chunk));
