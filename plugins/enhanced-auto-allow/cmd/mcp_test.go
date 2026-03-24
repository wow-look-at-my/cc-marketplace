package main

import (
	"testing"

	"github.com/wow-look-at-my/testify/assert"
)

func TestMatchMCPToolGlobPatterns(t *testing.T) {
	pats := []string{"get_*", "list_*", "search_*", "query_*"}

	for _, tc := range []struct {
		suffix string
		want   bool
	}{
		{"get_dashboard_by_uid", true},
		{"list_datasources", true},
		{"search_dashboards", true},
		{"query_prometheus", true},
		{"delete_dashboard", false},
		{"create_incident", false},
		{"", false},
	} {
		assert.Equal(t, tc.want, matchMCPTool(pats, tc.suffix), tc.suffix)
	}
}

func TestMatchMCPToolExactMatch(t *testing.T) {
	pats := []string{"special_tool"}
	assert.True(t, matchMCPTool(pats, "special_tool"))
	assert.False(t, matchMCPTool(pats, "special_tool_extra"))
}

func TestMatchMCPToolEmptyPatterns(t *testing.T) {
	assert.False(t, matchMCPTool(nil, "get_foo"))
	assert.False(t, matchMCPTool([]string{}, "get_foo"))
}

func TestMCPToolSuffix(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"mcp__grafana__get_dashboard_by_uid", "get_dashboard_by_uid"},
		{"mcp__abc123__list_datasources", "list_datasources"},
		{"mcp__x__y", "y"},
		{"mcp__", ""},
		{"mcp__x", ""},
		{"Read", ""},
		{"", ""},
	} {
		assert.Equal(t, tc.want, mcpToolSuffix(tc.input), tc.input)
	}
}
