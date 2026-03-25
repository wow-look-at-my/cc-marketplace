package main

import (
	"testing"

	"github.com/wow-look-at-my/testify/assert"
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
