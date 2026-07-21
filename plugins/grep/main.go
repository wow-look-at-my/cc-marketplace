// Command grep is a stdio MCP server restoring the builtin Grep tool
// that claude-code disabled in 2.1.117. Behavior mirrors 2.1.116 except
// for a redesigned output-mode set (see greptool.go).
package main

import (
	"log"
	"os"
)

func main() {
	logger := log.New(os.Stderr, "grep-mcp: ", 0)
	tool := newGrepTool(logger.Printf)
	srv := newServer(os.Stdin, os.Stdout, logger.Printf, "grep", []mcpTool{tool}, gateEnvVar)
	if err := srv.run(); err != nil {
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
