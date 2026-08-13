// Event dispatch. One binary, three events.
//
// Everything fails open: unparseable stdin, an unreadable directory, a vanished
// file all report nothing and exit 0. A guard that can wedge a session start is
// worse than no guard.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// hookInput is the payload Claude Code delivers on stdin. Every field is
// optional: absent cwd falls back to the process's, and an absent event means
// the session-start census, so the hook still works driven by hand.
//
// FullScan is never sent by Claude Code -- no real hook_event_name collides
// with it, and it needs none of the three real events' session-scoped state
// (a marker, a size+mtime snapshot). It exists for exactly one caller: a CI
// job that wants a real pass/fail signal for a whole tree, not advisory JSON
// for whatever files a live session happened to load or touch.
type hookInput struct {
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	StopHookActive bool   `json:"stop_hook_active"`
	FullScan       bool   `json:"full_scan"`
	ToolInput      struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

func parseInput(raw []byte) hookInput {
	in := hookInput{HookEventName: "SessionStart"}
	if err := json.Unmarshal(raw, &in); err != nil {
		// Garbage stdin is not an error worth reporting; defaults stand.
		return hookInput{HookEventName: "SessionStart", CWD: cwdOrDot()}
	}
	if in.CWD == "" {
		in.CWD = cwdOrDot()
	}
	if in.HookEventName == "" {
		in.HookEventName = "SessionStart"
	}
	return in
}

func cwdOrDot() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// findOffenders is the session-start census: the same recursive walk
// full_scan uses (allCandidatePaths), reported size only. Width is judged on
// what a session WRITES (see editReport) -- listing every pre-existing
// unwrapped file at session start buries the one file that matters under
// forty that do not, which is how a guard teaches the model to skim past it.
func findOffenders(cwd string, limit int) []offender {
	floor := nearLimit(limit)
	seen := map[string]bool{}
	var offenders []offender
	for _, path := range allCandidatePaths(cwd) {
		key, err := filepath.Abs(path)
		if err != nil || seen[key] {
			continue
		}
		seen[key] = true
		chars, _, ok := measure(path)
		if ok && chars >= floor {
			offenders = append(offenders, offender{Path: path, Chars: chars})
		}
	}
	worstFirst(offenders)
	return offenders
}

// editReport names the instruction files this tool call left near or over the
// wall, or unwrapped. The tool may name the file it wrote (Write/Edit) or name
// nothing at all (Bash) -- either way the answer comes from measuring.
func editReport(in hookInput, limit int) string {
	floor := nearLimit(limit)

	var paths []string
	if in.ToolInput.FilePath != "" && isInstructionFile(in.ToolInput.FilePath) {
		if abs, err := filepath.Abs(in.ToolInput.FilePath); err == nil {
			paths = []string{abs}
		}
		// Keep the snapshot current even when the tool named its file, so a
		// later Bash edit is diffed against the truth rather than a stale entry.
		changedFiles(in.SessionID, in.CWD)
	} else {
		for _, p := range changedFiles(in.SessionID, in.CWD) {
			if isInstructionFile(p) {
				paths = append(paths, p)
			}
		}
	}

	var offenders []offender
	for _, path := range paths {
		chars, wide, ok := measure(path)
		if !ok || (chars < floor && len(wide) == 0) {
			continue
		}
		recordOffender(in.SessionID, path)
		offenders = append(offenders, offender{Path: path, Chars: chars, Wide: wide})
	}
	if len(offenders) == 0 {
		return ""
	}
	worstFirst(offenders)
	worst := offenders[0]
	growth, hasGrowth := growthOverHead(worst.Path, worst.Chars)
	return editReportText(worst, limit, growth, hasGrowth)
}

// stopBlock decides whether to refuse the end of the turn. It fires once per
// (file, content): a file left as the gate found it never blocks twice, which is
// what makes a hard block safe, but re-breaking one is a new violation.
func stopBlock(in hookInput, limit int) string {
	if in.StopHookActive || in.SessionID == "" {
		return ""
	}
	m := readMarker(in.SessionID)
	if m == nil || len(m.Paths) == 0 {
		return ""
	}

	floor := nearLimit(limit)
	var still []offender
	for _, path := range m.Paths {
		chars, wide, ok := measure(path)
		if !ok || (chars < floor && len(wide) == 0) {
			continue
		}
		sig, hasSig := signature(path)
		if hasSig && m.Fired[path] == sig {
			continue // already said, unchanged since
		}
		if hasSig {
			m.Fired[path] = sig
		}
		still = append(still, offender{Path: path, Chars: chars, Wide: wide})
	}
	if len(still) == 0 {
		return ""
	}
	worstFirst(still)
	// Deliberately NOT clearing the marker: it also carries the size+mtime
	// snapshot the post-edit sweep diffs against and the list of files this
	// session has already broken once. Dropping it here disarms the guard for
	// the rest of the session the first time a turn ends cleanly.
	writeMarker(in.SessionID, m)
	return stopReason(still, limit)
}

// run returns the stdout payload and process exit code for a given input.
// Every real Claude Code event always exits 0 -- a size check must never
// break a session or a turn, so nothing here can fail a caller that reads
// only the JSON. full_scan is the one path that means anything by its exit
// code: it is not a session hook, it is CI, and CI needs a real signal.
func run(r io.Reader) (string, int) {
	limit := budget()
	if limit == 0 {
		return "", 0 // explicitly disabled
	}
	raw, _ := io.ReadAll(r)
	in := parseInput(raw)

	if in.FullScan {
		offenders := fullScanOffenders(in.CWD, limit)
		if len(offenders) == 0 {
			return "", 0
		}
		exit := 0
		for _, o := range offenders {
			if o.Chars > limit {
				exit = 1
				break
			}
		}
		// Plain text, not the hookSpecificOutput envelope: the reader here is
		// a CI log, not Claude Code's hook protocol.
		return sessionReport(offenders, limit) + "\n", exit
	}

	switch in.HookEventName {
	case "Stop":
		reason := stopBlock(in, limit)
		if reason == "" {
			return "", 0
		}
		return encode(map[string]any{"decision": "block", "reason": reason}), 0
	case "PostToolUse":
		return context(in.HookEventName, editReport(in, limit)), 0
	default:
		seedSnapshot(in.SessionID, in.CWD)
		offenders := findOffenders(in.CWD, limit)
		if len(offenders) == 0 {
			return "", 0
		}
		return context(in.HookEventName, sessionReport(offenders, limit)), 0
	}
}

func context(event, text string) string {
	if text == "" {
		return ""
	}
	return encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": text,
		},
	})
}

// encode renders a hook response, returning "" if it cannot be marshalled so a
// broken guard stays fail-open.
func encode(v map[string]any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data) + "\n"
}

func main() {
	// Fail open, unconditionally: this runs at the start of every session and
	// after every tool call. A size check must never break either.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "claude-md-budget: reporting nothing: %v\n", r)
		}
	}()
	out, code := run(os.Stdin)
	if out != "" {
		fmt.Print(out)
	}
	os.Exit(code)
}
