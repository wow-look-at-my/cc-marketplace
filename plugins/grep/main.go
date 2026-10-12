package main

import (
	"log"
	"os"

	"rgmcp"
)

// gateEnvVar is the escape hatch: CC_GREP_PLUGIN=always|never|auto (default auto).
const gateEnvVar = "CC_GREP_PLUGIN"

func main() {
	logger := log.New(os.Stderr, "grep-mcp: ", 0)
	tool := newGrepTool(logger.Printf)
	srv := rgmcp.NewServer(os.Stdin, os.Stdout, logger.Printf, "grep", []rgmcp.Tool{tool}, gateEnvVar)
	if err := srv.Run(); err != nil {
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
