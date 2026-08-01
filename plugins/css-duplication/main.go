// css-duplication: a language server that reports the same declaration block
// written under more than one selector.
//
// A language server rather than a hook on purpose: diagnostics land in context
// by themselves after an edit, anchored to the offending selector, and clear
// themselves when the block is hoisted -- no message shouted at the end of a
// tool call, nothing to dismiss, no separate call to make.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := NewServer(os.Stdout).Serve(os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "css-duplication:", err)
		os.Exit(1)
	}
}
