package main

import (
	"log"
	"os"
)

func main() {
	logger := log.New(os.Stderr, "glob-mcp: ", 0)
	tool := newGlobTool(logger.Printf)
	srv := newServer(os.Stdin, os.Stdout, logger.Printf, "glob", []mcpTool{tool}, gateEnvVar)
	if err := srv.run(); err != nil {
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
