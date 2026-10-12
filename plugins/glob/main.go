package main

import (
	"log"
	"os"

	"rgmcp"
)

// gateEnvVar is the escape hatch: CC_GLOB_PLUGIN=always|never|auto (default auto).
const gateEnvVar = "CC_GLOB_PLUGIN"

func main() {
	logger := log.New(os.Stderr, "glob-mcp: ", 0)
	tool := newGlobTool(logger.Printf)
	srv := rgmcp.NewServer(os.Stdin, os.Stdout, logger.Printf, "glob", []rgmcp.Tool{tool}, gateEnvVar)
	if err := srv.Run(); err != nil {
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
