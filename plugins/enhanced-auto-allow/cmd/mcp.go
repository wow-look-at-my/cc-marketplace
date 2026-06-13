package main

import (
	"path"
	"strings"
)

func parseMCPTool(toolName string) (server, tool string) {
	if !strings.HasPrefix(toolName, "mcp__") {
		return "", ""
	}
	lastIdx := strings.LastIndex(toolName, "__")
	if lastIdx <= 4 {
		return "", ""
	}
	return toolName[5:lastIdx], toolName[lastIdx+2:]
}

func matchMCPServer(servers map[string][]string, server, tool string) bool {
	for serverPat, toolPats := range servers {
		if matched, _ := path.Match(serverPat, server); !matched {
			continue
		}
		for _, toolPat := range toolPats {
			if matched, _ := path.Match(toolPat, tool); matched {
				return true
			}
		}
	}
	return false
}
