package main

import (
	"os"

	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMCPTool(t *testing.T) {
	for _, tc := range []struct {
		input      string
		wantServer string
		wantTool   string
	}{
		{"mcp__grafana__get_dashboard_by_uid", "grafana", "get_dashboard_by_uid"},
		{"mcp__abc123__list_datasources", "abc123", "list_datasources"},
		{"mcp__x__y", "x", "y"},
		{"mcp__", "", ""},
		{"mcp__x", "", ""},
		{"Read", "", ""},
		{"", "", ""},
	} {
		server, tool := parseMCPTool(tc.input)
		assert.Equal(t, tc.wantServer, server, tc.input+" server")
		assert.Equal(t, tc.wantTool, tool, tc.input+" tool")
	}
}

func TestMatchMCPServerGrafana(t *testing.T) {
	servers := map[string][]string{
		"grafana": {"get_*", "list_*", "search_*", "query_*"},
	}

	for _, tc := range []struct {
		server, tool string
		want         bool
	}{
		{"grafana", "get_dashboard_by_uid", true},
		{"grafana", "list_datasources", true},
		{"grafana", "search_dashboards", true},
		{"grafana", "query_prometheus", true},
		{"grafana", "delete_dashboard", false},
		{"grafana", "create_incident", false},
		{"other-server", "get_something", false},
		{"other-server", "list_things", false},
	} {
		assert.Equal(t, tc.want, matchMCPServer(servers, tc.server, tc.tool),
			tc.server+"__"+tc.tool)
	}
}

func TestMatchMCPServerWildcard(t *testing.T) {
	servers := map[string][]string{
		"*": {"get_*"},
	}
	assert.True(t, matchMCPServer(servers, "any-server", "get_foo"))
	assert.False(t, matchMCPServer(servers, "any-server", "delete_foo"))
}

func TestMatchMCPServerMultiple(t *testing.T) {
	servers := map[string][]string{
		"grafana":    {"get_*", "list_*"},
		"cloudflare": {"search_*"},
	}
	assert.True(t, matchMCPServer(servers, "grafana", "get_dashboard"))
	assert.True(t, matchMCPServer(servers, "cloudflare", "search_docs"))
	assert.False(t, matchMCPServer(servers, "grafana", "search_docs"))
	assert.False(t, matchMCPServer(servers, "cloudflare", "get_dashboard"))
}

func TestMatchMCPServerEmpty(t *testing.T) {
	assert.False(t, matchMCPServer(nil, "grafana", "get_foo"))
	assert.False(t, matchMCPServer(map[string][]string{}, "grafana", "get_foo"))
}

func TestMatchMCPServerExactTool(t *testing.T) {
	servers := map[string][]string{
		"github": {"issue_read", "pull_request_read"},
	}
	assert.True(t, matchMCPServer(servers, "github", "issue_read"))
	assert.True(t, matchMCPServer(servers, "github", "pull_request_read"))
	assert.False(t, matchMCPServer(servers, "github", "issue_write"))
}

// The shipped rules must auto-allow read-only tools on servers nobody
// enumerated in advance; the old server-by-server list left everything else
// asking.
func TestShippedRulesAllowReadOnlyMCPTools(t *testing.T) {
	data, err := os.ReadFile("../rules.xml")
	assert.NoError(t, err)
	r, err := loadXMLRules(data)
	assert.NoError(t, err)

	for _, tc := range []struct {
		toolName string
		want     bool
	}{
		{"mcp__github__get_file_contents", true},
		{"mcp__github__list_pull_requests", true},
		{"mcp__github__search_code", true},
		{"mcp__github__pull_request_read", true},
		{"mcp__github__actions_list", true},
		{"mcp__github__actions_run_trigger", false},
		{"mcp__github__issue_write", false},
		{"mcp__github__update_pull_request", false},
		{"mcp__grafana__query_prometheus", true},
		{"mcp__grafana__delete_dashboard", false},
		{"mcp__Context7_Pro__resolve-library-id", true},
		{"mcp__Context7_Pro__query-docs", true},
		{"mcp__Playwright__browser_snapshot", true},
		{"mcp__Playwright__browser_click", false},
		{"mcp__Playwright__browser_evaluate", false},
		{"mcp__plugin_grep_grep__Grep", true},
		{"mcp__plugin_glob_glob__Glob", true},
		// No server-name wildcard: an unlisted server is never auto-allowed,
		// however read-only its tool names look.
		{"mcp__CONTENTdm__search_items", false},
		{"mcp__Robinhood__get_equity_quotes", false},
		{"mcp__Robinhood__place_equity_order", false},
	} {
		server, tool := parseMCPTool(tc.toolName)
		assert.Equal(t, tc.want, matchMCPServer(r.MCPServers, server, tool), tc.toolName)
	}
}

func TestLoadXMLRulesMCPServers(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<rules>
  <mcpServer name="grafana">
    <tool>get_*</tool>
    <tool>list_*</tool>
  </mcpServer>
  <mcpServer name="cloudflare">
    <tool>search_*</tool>
  </mcpServer>
</rules>`
	rules, err := loadXMLRules([]byte(xml))
	assert.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"grafana":    {"get_*", "list_*"},
		"cloudflare": {"search_*"},
	}, rules.MCPServers)
}
