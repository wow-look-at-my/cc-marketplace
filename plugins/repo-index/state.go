package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// statePath keys the record by a hash of the session id, so a session id that
// contains a path separator cannot escape the temp directory.
func statePath(dir, sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(dir, "repo-index", hex.EncodeToString(sum[:])[:16]+".json")
}

type state struct {
	Suggested []string `json:"suggested"`
}

// readState returns the repos this session already saw. A corrupt record reads
// as empty, which repeats a suggestion at worst.
func readState(dir, sessionID string) map[string]bool {
	seen := map[string]bool{}
	data, err := os.ReadFile(statePath(dir, sessionID))
	if err != nil {
		return seen
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return seen
	}
	for _, name := range s.Suggested {
		seen[name] = true
	}
	return seen
}

// writeState records the full set for the session. The error travels to the
// caller: the once-per-session promise is the whole feature, so a lost record
// is worth a word on stderr rather than silence.
func writeState(dir, sessionID string, seen map[string]bool) error {
	path := statePath(dir, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	s := state{Suggested: make([]string, 0, len(seen))}
	for name := range seen {
		s.Suggested = append(s.Suggested, name)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("cannot encode state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}
