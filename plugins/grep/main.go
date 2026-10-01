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
