package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// frame wraps a JSON-RPC body in the LSP header framing a real client sends.
func frame(t *testing.T, msg map[string]any) string {
	t.Helper()
	b, err := json.Marshal(msg)
	require.NoError(t, err)
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(b), b)
}

// drain reads every frame the server wrote.
func drain(t *testing.T, out *bytes.Buffer) []rpcMessage {
	t.Helper()
	var msgs []rpcMessage
	r := bufio.NewReader(bytes.NewReader(out.Bytes()))
	for {
		body, err := readMessage(r)
		if err != nil {
			return msgs
		}
		var m rpcMessage
		require.NoError(t, json.Unmarshal(body, &m))
		msgs = append(msgs, m)
	}
}

func publishedFor(t *testing.T, msgs []rpcMessage, uri string) []diagnostic {
	t.Helper()
	var found []diagnostic
	for _, m := range msgs {
		if m.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p struct {
			URI         string       `json:"uri"`
			Diagnostics []diagnostic `json:"diagnostics"`
		}
		require.NoError(t, json.Unmarshal(m.Params, &p))
		if p.URI == uri {
			found = p.Diagnostics
		}
	}
	return found
}

func TestServerHandshakeAdvertisesSyncAndDiagnostics(t *testing.T) {
	var out bytes.Buffer
	in := frame(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}}) +
		frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	require.NoError(t, NewServer(&out).Serve(strings.NewReader(in)))

	msgs := drain(t, &out)
	require.Len(t, msgs, 1, "a request must be answered")
	raw, err := json.Marshal(msgs[0].Result)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"diagnosticProvider"`)
	require.Contains(t, string(raw), `"textDocumentSync"`)
	require.Contains(t, string(raw), `"includeText":true`)
}

// Every response must carry `result` or `error` -- a success response with
// NEITHER is malformed, and vscode-jsonrpc (what Claude Code uses) rejects it
// with "The received response has neither a result nor an error property".
// This is asserted on the RAW JSON on purpose: unmarshalling into a struct
// silently turns a missing key into a zero value, which is exactly why the
// original shutdown bug survived a green test suite and only showed up when a
// real client tried to stop the server.
func TestEveryResponseCarriesResultOrError(t *testing.T) {
	var out bytes.Buffer
	in := frame(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}}) +
		frame(t, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"}) +
		frame(t, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/rename", "params": map[string]any{}}) +
		frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	require.NoError(t, NewServer(&out).Serve(strings.NewReader(in)))

	r := bufio.NewReader(bytes.NewReader(out.Bytes()))
	seen := 0
	for {
		body, err := readMessage(r)
		if err != nil {
			break
		}
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &raw))
		if _, isResponse := raw["id"]; !isResponse {
			continue
		}
		seen++
		_, hasResult := raw["result"]
		_, hasError := raw["error"]
		require.True(t, hasResult || hasError, "response without result or error: %s", body)
		require.False(t, hasResult && hasError, "response with both: %s", body)
	}
	require.Equal(t, 3, seen, "initialize, shutdown and the unknown method are all answered")
}

func TestDidOpenPublishesOneDiagnosticPerCopy(t *testing.T) {
	uri := "file:///repo/dashboard.css"
	var out bytes.Buffer
	in := frame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "text": dashboardCSS},
	}}) + frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	require.NoError(t, NewServer(&out).Serve(strings.NewReader(in)))

	diags := publishedFor(t, drain(t, &out), uri)
	require.NotEmpty(t, diags)

	// Every copy gets its own marker -- a single diagnostic on one arbitrary
	// rule would leave the others looking fine.
	var lead, pointers int
	for _, d := range diags {
		require.Equal(t, severityWarning, d.Severity)
		require.Equal(t, diagnosticSource, d.Source)
		require.Contains(t, d.Message, "/docs:css-cascade")
		require.NotEmpty(t, d.RelatedInformation, "the other copies are linked")
		if strings.Contains(d.Message, "text-decoration: none } also on") {
			lead++
			require.Contains(t, d.Message, "4 copies")
		}
		if strings.Contains(d.Message, "same block as line") {
			pointers++
		}
	}
	require.Equal(t, 1, lead, "the body and sibling list are spelled out exactly once")
	require.NotZero(t, pointers, "the other copies point back at it")
}

// The client concatenates diagnostics into ONE block and truncates it at 4000
// characters (10 per file, 30 overall), so a chatty message costs the findings
// below it their place. This pins the budget rather than trusting prose.
func TestDiagnosticMessagesStayInsideTheInjectionBudget(t *testing.T) {
	uri := "file:///repo/dashboard.css"
	s := NewServer(&bytes.Buffer{})
	s.setDoc(uri, dashboardCSS)

	diags := s.diagnose(uri)
	require.NotEmpty(t, diags)

	total := 0
	for _, d := range diags {
		require.Less(t, len(d.Message), 200, "one diagnostic must not eat the block: %q", d.Message)
		total += len(d.Message)
	}
	// What a client would actually inject: the first ten of this file.
	injected := 0
	for i, d := range diags {
		if i == 10 {
			break
		}
		injected += len(d.Message)
	}
	require.Less(t, injected, 4000, "the ten diagnostics a client injects must fit its 4000-char cap")
}

// Ranking has to hold at the diagnostic level too, because only the first ten
// of a file survive: the strongest finding must not be crowded out by copies
// of a weaker one.
func TestStrongestFindingsAreEmittedFirst(t *testing.T) {
	uri := "file:///repo/x.css"
	src := `
.a { border-bottom: 0; }
.b { border-bottom: 0; }
.c { border-bottom: 0; }
.d { color: var(--accent); text-decoration: none; }
.e { color: var(--accent); text-decoration: none; }
.f { color: var(--accent); text-decoration: none; }
.g { color: var(--accent); text-decoration: none; }`
	s := NewServer(&bytes.Buffer{})
	s.setDoc(uri, src)

	diags := s.diagnose(uri)
	require.Len(t, diags, 7)
	require.Contains(t, diags[0].Message, "text-decoration: none")
}

func TestDiagnosticRangePointsAtTheSelector(t *testing.T) {
	uri := "file:///repo/x.css"
	src := "\n\n.crumbs a { color: var(--accent); text-decoration: none; }\n" +
		".other a { color: var(--accent); text-decoration: none; }\n"

	s := NewServer(&bytes.Buffer{})
	s.setDoc(uri, src)
	diags := s.diagnose(uri)
	require.Len(t, diags, 2)

	// Line 3 of the file, 0-based line 2, starting at column 0.
	require.Equal(t, 2, diags[0].Range.Start.Line)
	require.Equal(t, 0, diags[0].Range.Start.Character)
	require.Equal(t, len(".crumbs a"), diags[0].Range.End.Character)
}

func TestDidChangeRecomputesAndClearsWhenFixed(t *testing.T) {
	uri := "file:///repo/x.css"
	dup := ".a { color: red; padding: 1px; }\n.b { color: red; padding: 1px; }\n"
	fixed := ".a, .b { color: red; padding: 1px; }\n"

	var out bytes.Buffer
	in := frame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "text": dup},
	}}) + frame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
		"textDocument":   map[string]any{"uri": uri},
		"contentChanges": []map[string]any{{"text": fixed}},
	}}) + frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	require.NoError(t, NewServer(&out).Serve(strings.NewReader(in)))

	msgs := drain(t, &out)
	require.Len(t, msgs, 2, "one publish per document state")
	require.Empty(t, publishedFor(t, msgs[1:], uri), "hoisting the block clears the warning")
}

func TestPullDiagnosticsMatchThePushedOnes(t *testing.T) {
	uri := "file:///repo/dashboard.css"
	var out bytes.Buffer
	in := frame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "text": dashboardCSS},
	}}) + frame(t, map[string]any{"jsonrpc": "2.0", "id": 7, "method": "textDocument/diagnostic", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}}) + frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	require.NoError(t, NewServer(&out).Serve(strings.NewReader(in)))

	msgs := drain(t, &out)
	pushed := publishedFor(t, msgs, uri)

	var report struct {
		Kind  string       `json:"kind"`
		Items []diagnostic `json:"items"`
	}
	for _, m := range msgs {
		if len(m.ID) > 0 {
			raw, err := json.Marshal(m.Result)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &report))
		}
	}
	require.Equal(t, "full", report.Kind)
	require.Equal(t, len(pushed), len(report.Items))
}

func TestDidCloseClearsDiagnostics(t *testing.T) {
	uri := "file:///repo/x.css"
	var out bytes.Buffer
	in := frame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri, "text": ".a { color: red; padding: 1px; }\n.b { color: red; padding: 1px; }"},
	}}) + frame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didClose", "params": map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}}) + frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	require.NoError(t, NewServer(&out).Serve(strings.NewReader(in)))

	msgs := drain(t, &out)
	require.NotEmpty(t, msgs)
	require.Empty(t, publishedFor(t, msgs, uri), "the last publish for a closed file is empty")
}

func TestServerSurvivesGarbageAndUnknownMethods(t *testing.T) {
	var out bytes.Buffer
	in := "Content-Length: 7\r\n\r\nnotjson" +
		frame(t, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/rename", "params": map[string]any{}}) +
		frame(t, map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{}}) +
		frame(t, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "shutdown"}) +
		frame(t, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	require.NoError(t, NewServer(&out).Serve(strings.NewReader(in)))

	msgs := drain(t, &out)
	require.Len(t, msgs, 2, "the unknown request and shutdown are both answered")
	require.NotNil(t, msgs[0].Error, "an unknown REQUEST gets an error, never silence")
	require.Equal(t, -32601, msgs[0].Error.Code)
	require.Nil(t, msgs[1].Error, "shutdown succeeds")
}

func TestNonStylesheetURIsAreIgnored(t *testing.T) {
	s := NewServer(&bytes.Buffer{})
	s.setDoc("file:///repo/x.scss", ".a { color: red; padding: 1px; }\n.b { color: red; padding: 1px; }")
	require.Empty(t, s.diagnose("file:///repo/x.scss"))
	require.Empty(t, s.diagnose("file:///repo/never-opened.css"))
}
