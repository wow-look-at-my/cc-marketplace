// Per-session state: which instruction files THIS session left too big, and
// what every candidate looked like last time we checked.
//
// Keyed per session so parallel sessions never collide, in tmp because it is
// turn-scoped state, not anything worth persisting.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// marker is the session's accumulated view.
//
// Seen is the size+mtime of every candidate as of the last check, which is what
// lets the post-edit sweep find a file written by a tool that names no path.
// Fired records the signature the Stop gate last blocked on PER FILE, not a
// single boolean: blocking once per session meant that after one block a session
// could bloat a second file -- or re-break the same one -- and end the turn in
// silence. Keying on the signature keeps the no-wedge property (a file left
// untouched never blocks twice) while making a NEW violation always audible.
type marker struct {
	Paths []string          `json:"paths"`
	Fired map[string]string `json:"fired"`
	Seen  map[string]string `json:"seen"`
}

func newMarker() *marker {
	return &marker{Fired: map[string]string{}, Seen: map[string]string{}}
}

func markerPath(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(os.TempDir(), "claude-md-budget", hex.EncodeToString(sum[:])[:16]+".json")
}

func readMarker(sessionID string) *marker {
	data, err := os.ReadFile(markerPath(sessionID))
	if err != nil {
		return nil
	}
	var m marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	if m.Fired == nil {
		m.Fired = map[string]string{}
	}
	if m.Seen == nil {
		m.Seen = map[string]string{}
	}
	return &m
}

// writeMarker persists the marker. Losing it costs a nag, not correctness.
func writeMarker(sessionID string, m *marker) {
	path := markerPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func recordOffender(sessionID, path string) {
	if sessionID == "" {
		return
	}
	m := readMarker(sessionID)
	if m == nil {
		m = newMarker()
	}
	for _, p := range m.Paths {
		if p == path {
			return
		}
	}
	m.Paths = append(m.Paths, path)
	writeMarker(sessionID, m)
}

// seedSnapshot records what every candidate looked like BEFORE this session
// touched anything, so the first post-edit sweep has something to diff against.
// Without it the first Bash-written edit of a session is the one that gets away
// -- and the first edit is usually the one that does the damage.
func seedSnapshot(sessionID, cwd string) {
	if sessionID == "" {
		return
	}
	m := readMarker(sessionID)
	if m == nil {
		m = newMarker()
	}
	m.Seen = snapshot(cwd)
	writeMarker(sessionID, m)
}

func snapshot(cwd string) map[string]string {
	seen := map[string]string{}
	for _, path := range candidates(cwd) {
		key, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, done := seen[key]; done {
			continue
		}
		if sig, ok := signature(key); ok {
			seen[key] = sig
		}
	}
	return seen
}

// changedFiles refreshes the snapshot and returns the instruction files that
// CHANGED since the last check. Watching files rather than tool_input.file_path
// is what makes the guard un-walk-aroundable: a Bash edit names no path.
func changedFiles(sessionID, cwd string) []string {
	if sessionID == "" {
		return nil
	}
	m := readMarker(sessionID)
	if m == nil {
		m = newMarker()
	}
	first := len(m.Seen) == 0
	seen := snapshot(cwd)

	var changed []string
	if !first {
		for key, sig := range seen {
			if m.Seen[key] != sig {
				changed = append(changed, key)
			}
		}
	}
	m.Seen = seen
	writeMarker(sessionID, m)
	return changed
}
