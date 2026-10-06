// gate.go decides whether the tool is exposed to the connected client.
//
// A gated-off server answers tools/list with an empty list, which
// claude-code fully supports. Each plugin passes its own escape-hatch
// variable (CC_<NAME>_PLUGIN=always|never|auto, default auto) to NewServer.
package rgmcp

import (
	"strconv"
	"strings"
)

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

// parseSemver extracts a numeric major.minor.patch prefix.
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
