package main

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in   string
		want semver
		ok   bool
	}{
		{"2.1.116", semver{2, 1, 116}, true},
		{"2.1.117", semver{2, 1, 117}, true},
		{"0.0.0", semver{0, 0, 0}, true},
		{"10.20.30", semver{10, 20, 30}, true},
		{"v2.1.116", semver{2, 1, 116}, true},
		{" 2.1.116 ", semver{2, 1, 116}, true},
		{"2.1.116-beta.1", semver{2, 1, 116}, true},
		{"2.1.116+build.5", semver{2, 1, 116}, true},
		{"2.1.116.4", semver{2, 1, 116}, true}, // extra component ignored as suffix
		{"", semver{}, false},
		{"2", semver{}, false},
		{"2.1", semver{}, false},
		{"2.1.", semver{}, false},
		{"garbage", semver{}, false},
		{"a.b.c", semver{}, false},
		{"2..117", semver{}, false},
		{"-2.1.117", semver{}, false},
	}
	for _, tc := range cases {
		got, ok := parseSemver(tc.in)
		assert.False(t, ok != tc.ok || (ok && got != tc.want))

	}
}

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b semver
		want bool
	}{
		{semver{2, 1, 116}, semver{2, 1, 117}, true},
		{semver{2, 1, 117}, semver{2, 1, 117}, false},
		{semver{2, 1, 118}, semver{2, 1, 117}, false},
		{semver{2, 0, 999}, semver{2, 1, 117}, true},
		{semver{1, 99, 99}, semver{2, 1, 117}, true},
		{semver{3, 0, 0}, semver{2, 1, 117}, false},
		{semver{2, 2, 0}, semver{2, 1, 117}, false},
	}
	for _, tc := range cases {
		got := tc.a.less(tc.b)
		assert.Equal(t, tc.want, got)

	}
}

func TestGateAllows(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		client  string
		version string
		want    bool
	}{
		{"auto: claude-code with builtin", "", "claude-code", "2.1.116", false},
		{"auto: claude-code first removed", "", "claude-code", "2.1.117", true},
		{"auto: claude-code current", "", "claude-code", "2.1.207", true},
		{"auto: unknown client", "", "mcp-inspector", "0.1.0", true},
		{"auto: garbage version", "", "claude-code", "not-semver", true},
		{"auto: empty everything", "", "", "", true},
		{"explicit auto", "auto", "claude-code", "2.1.116", false},
		{"always wins", "always", "claude-code", "2.1.116", true},
		{"never wins", "never", "claude-code", "2.1.207", false},
		{"case-insensitive always", "AlWaYs", "claude-code", "2.1.116", true},
		{"whitespace-tolerant never", " never ", "other", "1.0.0", false},
		{"unrecognized mode falls back to auto", "sometimes", "claude-code", "2.1.116", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gateAllows(tc.mode, tc.client, tc.version)
			assert.Equal(t, tc.want, got)

		})
	}
}
