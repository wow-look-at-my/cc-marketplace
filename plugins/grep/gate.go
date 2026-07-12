// gate.go decides whether the tool is exposed to the connected client.
//
// claude-code shipped a builtin Grep through 2.1.116 and disabled it by
// default starting with 2.1.117 (the definitions remain behind opt-ins
// through at least 2.1.207). Exposing a second Glob to an old client is
// harmless-but-redundant, so the gate hides the tool only when it is sure
// the builtin is present: clientInfo.name == "claude-code" AND the version
// parses AND it is < 2.1.117. Unknown clients, missing or garbage
// versions, and >= 2.1.117 all expose the tool. A gated-off server answers
// tools/list with an empty list, which claude-code fully supports.
//
// A sibling plugin copies this file verbatim and changes only gateEnvVar.
package main

import (
	"strconv"
	"strings"
)

// gateEnvVar is the escape hatch: CC_GREP_PLUGIN=always|never|auto
// (default auto). It is checked before the clientInfo rule.
const gateEnvVar = "CC_GREP_PLUGIN"

// builtinRemovedIn is the first claude-code version whose builtin was
// disabled by default (spec: 2.1.117:cli.js:114098-114102).
var builtinRemovedIn = semver{2, 1, 117}

type semver struct {
	major, minor, patch int
}

func (v semver) less(o semver) bool {
	if v.major != o.major {
		return v.major < o.major
	}
	if v.minor != o.minor {
		return v.minor < o.minor
	}
	return v.patch < o.patch
}

// parseSemver extracts a numeric major.minor.patch prefix. Any suffix
// ("-beta.1", "+build", trailing junk) is ignored; anything without three
// leading numeric components fails.
func parseSemver(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	var parts [3]int
	for i := 0; i < 3; i++ {
		j := 0
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == 0 {
			return semver{}, false
		}
		n, err := strconv.Atoi(s[:j])
		if err != nil {
			return semver{}, false
		}
		parts[i] = n
		s = s[j:]
		if i < 2 {
			if !strings.HasPrefix(s, ".") {
				return semver{}, false
			}
			s = s[1:]
		}
	}
	return semver{parts[0], parts[1], parts[2]}, true
}

// gateAllows reports whether the tool should be exposed for the given
// escape-hatch mode and clientInfo.
func gateAllows(mode, clientName, clientVersion string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always":
		return true
	case "never":
		return false
	}
	// auto (also the fallback for unrecognized modes)
	if clientName != "claude-code" {
		return true
	}
	v, ok := parseSemver(clientVersion)
	if !ok {
		return true
	}
	return !v.less(builtinRemovedIn)
}
