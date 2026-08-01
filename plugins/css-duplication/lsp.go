package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// A language server, not a hook: diagnostics arrive in context on their own
// after an edit instead of a message shouted at the end of a tool call. The
// detector is shared with nothing else to keep the two in agreement -- css.go
// is the single implementation.

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type position struct {
	Line      int `json:"line"`      // 0-based
	Character int `json:"character"` // 0-based, UTF-16 units
}

type rng struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type location struct {
	URI   string `json:"uri"`
	Range rng    `json:"range"`
}

type relatedInfo struct {
	Location location `json:"location"`
	Message  string   `json:"message"`
}

type diagnostic struct {
	Range              rng           `json:"range"`
	Severity           int           `json:"severity"` // 2 = Warning
	Source             string        `json:"source"`
	Message            string        `json:"message"`
	RelatedInformation []relatedInfo `json:"relatedInformation,omitempty"`
}

type textDocumentItem struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

// Server holds the open documents. A language server is long-lived and is
// spoken to concurrently in principle, so the map is guarded even though the
// client drives it from one connection.
type Server struct {
	out io.Writer

	mu   sync.Mutex
	docs map[string]string // uri -> text
}

func NewServer(out io.Writer) *Server {
	return &Server{out: out, docs: map[string]string{}}
}

const (
	severityWarning  = 2
	diagnosticSource = "css-duplication"
)

// stylesheetExts is deliberately narrow, and matches what .lsp.json registers.
// A preprocessor's nesting changes what a duplicate body MEANS (`&:hover`
// under two parents is not a repeated rule), so this parser only claims plain
// CSS.
var stylesheetExts = map[string]bool{".css": true}

// Serve runs the stdio JSON-RPC loop until the stream ends or `exit` arrives.
func (s *Server) Serve(in io.Reader) error {
	r := bufio.NewReader(in)
	for {
		body, err := readMessage(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			continue // a malformed frame is not worth killing the server over
		}
		stop, result, rerr := s.dispatch(msg)
		if len(msg.ID) > 0 { // a request: it must be answered, even with null
			s.respond(msg.ID, result, rerr)
		}
		if stop {
			return nil
		}
	}
}

func (s *Server) dispatch(msg rpcMessage) (stop bool, result any, rerr *rpcError) {
	switch msg.Method {
	case "initialize":
		return false, initializeResult(), nil

	case "initialized", "workspace/didChangeConfiguration", "$/setTrace":
		return false, nil, nil

	case "textDocument/didOpen":
		var p struct {
			TextDocument textDocumentItem `json:"textDocument"`
		}
		if err := json.Unmarshal(msg.Params, &p); err == nil {
			s.setDoc(p.TextDocument.URI, p.TextDocument.Text)
			s.publish(p.TextDocument.URI)
		}
		return false, nil, nil

	case "textDocument/didChange":
		// Full sync only (that is what the capabilities advertise), so the last
		// content change is the whole document.
		var p struct {
			TextDocument   textDocumentItem `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if err := json.Unmarshal(msg.Params, &p); err == nil && len(p.ContentChanges) > 0 {
			s.setDoc(p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text)
			s.publish(p.TextDocument.URI)
		}
		return false, nil, nil

	case "textDocument/didSave":
		var p struct {
			TextDocument textDocumentItem `json:"textDocument"`
			Text         string           `json:"text"`
		}
		if err := json.Unmarshal(msg.Params, &p); err == nil {
			if p.Text != "" {
				s.setDoc(p.TextDocument.URI, p.Text)
			}
			s.publish(p.TextDocument.URI)
		}
		return false, nil, nil

	case "textDocument/didClose":
		var p struct {
			TextDocument textDocumentItem `json:"textDocument"`
		}
		if err := json.Unmarshal(msg.Params, &p); err == nil {
			s.mu.Lock()
			delete(s.docs, p.TextDocument.URI)
			s.mu.Unlock()
			// Clear what we published, so a closed file leaves no ghosts.
			s.notify("textDocument/publishDiagnostics", map[string]any{
				"uri": p.TextDocument.URI, "diagnostics": []diagnostic{},
			})
		}
		return false, nil, nil

	// Pull diagnostics: a client that prefers asking gets the same answer the
	// push path would have sent.
	case "textDocument/diagnostic":
		var p struct {
			TextDocument textDocumentItem `json:"textDocument"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return false, nil, &rpcError{Code: -32602, Message: "invalid params"}
		}
		return false, map[string]any{
			"kind":  "full",
			"items": s.diagnose(p.TextDocument.URI),
		}, nil

	case "shutdown":
		return false, nil, nil

	case "exit":
		return true, nil, nil
	}

	if len(msg.ID) > 0 {
		return false, nil, &rpcError{Code: -32601, Message: "method not found: " + msg.Method}
	}
	return false, nil, nil
}

func initializeResult() map[string]any {
	return map[string]any{
		"capabilities": map[string]any{
			// 1 = full document sync. The detector needs the whole stylesheet
			// anyway (a duplicate is a relationship between distant rules), so
			// incremental sync would buy nothing but bookkeeping.
			"textDocumentSync": map[string]any{
				"openClose": true,
				"change":    1,
				"save":      map[string]any{"includeText": true},
			},
			"diagnosticProvider": map[string]any{
				"identifier":            diagnosticSource,
				"interFileDependencies": false,
				"workspaceDiagnostics":  false,
			},
		},
		"serverInfo": map[string]any{"name": "css-duplication", "version": "1"},
	}
}

func (s *Server) setDoc(uri, text string) {
	s.mu.Lock()
	s.docs[uri] = text
	s.mu.Unlock()
}

func (s *Server) publish(uri string) {
	s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         uri,
		"diagnostics": s.diagnose(uri),
	})
}

// diagnose is the whole point: one diagnostic per copy, each naming the other
// selectors carrying the identical block, with the fix in the message.
func (s *Server) diagnose(uri string) []diagnostic {
	s.mu.Lock()
	text, ok := s.docs[uri]
	s.mu.Unlock()
	if !ok || !isStylesheet(uri) {
		return []diagnostic{}
	}

	lines := strings.Split(text, "\n")
	out := []diagnostic{}
	for _, g := range FindDuplicates(ParseRules(text)) {
		body := "{ " + strings.Join(g.Decls, "; ") + " }"
		for i, r := range g.Rules {
			var related []relatedInfo
			for _, other := range g.Rules {
				if other.Line == r.Line && other.Selector == r.Selector {
					continue
				}
				related = append(related, relatedInfo{
					Location: location{URI: uri, Range: selectorRange(lines, other)},
					Message:  "same block here: " + other.Selector,
				})
			}
			out = append(out, diagnostic{
				Range:    selectorRange(lines, r),
				Severity: severityWarning,
				Source:   diagnosticSource,
				// Terse on purpose, and said in full exactly ONCE per finding.
				// The client concatenates diagnostics into one block, keeps ten
				// per file and truncates at 4000 characters, so restating the
				// body and the sibling list on every copy would spend a
				// finding's whole budget on repeating itself. The first copy
				// carries the detail; the rest point at it, which is also how a
				// human reads it -- one explanation, several markers.
				Message:            groupMessage(g, i, body),
				RelatedInformation: related,
			})
		}
	}
	return out
}

// maxNamedSiblings bounds the "also on" list. A table rule can share its body
// with a dozen selectors, and the tail of that list teaches nothing the count
// does not -- while it does crowd out the next finding.
const maxNamedSiblings = 3

// groupMessage writes the finding once, on its first copy, and makes every
// later copy a pointer back to it.
func groupMessage(g Group, i int, body string) string {
	if i > 0 {
		return fmt.Sprintf("same block as line %d -- %d copies; hoist to one rule (/docs:css-cascade)",
			g.Rules[0].Line, len(g.Rules))
	}
	others := make([]string, 0, len(g.Rules)-1)
	for _, other := range g.Rules[1:] {
		others = append(others, other.Selector)
	}
	named := others
	if len(named) > maxNamedSiblings {
		named = append(append([]string(nil), others[:maxNamedSiblings]...),
			fmt.Sprintf("+%d more", len(others)-maxNamedSiblings))
	}
	return fmt.Sprintf("%s also on %s -- %d copies; hoist to one rule (/docs:css-cascade)",
		body, strings.Join(named, ", "), len(g.Rules))
}

// selectorRange points at the selector text itself when it can be found on its
// line, and at the whole line otherwise -- a diagnostic with a bogus range is
// worse than a coarse one.
func selectorRange(lines []string, r Rule) rng {
	idx := r.Line - 1
	if idx < 0 || idx >= len(lines) {
		return rng{}
	}
	line := lines[idx]
	// A multi-selector rule is stored whitespace-collapsed, so match its first
	// token rather than the reassembled string.
	needle := r.Selector
	if i := strings.IndexAny(needle, ",\n"); i > 0 {
		needle = needle[:i]
	}
	start := strings.Index(line, needle)
	if start < 0 {
		return rng{Start: position{Line: idx}, End: position{Line: idx, Character: len(line)}}
	}
	return rng{
		Start: position{Line: idx, Character: start},
		End:   position{Line: idx, Character: start + len(needle)},
	}
}

func isStylesheet(uri string) bool {
	path := uri
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		path = u.Path
	}
	return stylesheetExts[strings.ToLower(filepath.Ext(path))]
}

func (s *Server) notify(method string, params any) {
	raw, err := json.Marshal(params)
	if err != nil {
		return
	}
	s.send(rpcMessage{JSONRPC: "2.0", Method: method, Params: raw})
}

// respond answers a request. `result` is written even when it is nil, as an
// explicit JSON null: a success response carrying NEITHER result nor error is
// malformed, and a real client rejects it -- vscode-jsonrpc fails the request
// with "The received response has neither a result nor an error property",
// which is how `shutdown` was breaking after a clean, fully working session.
// Struct tags cannot express this (`omitempty` drops the null, dropping it is
// the bug), so the response is assembled by hand.
func (s *Server) respond(id json.RawMessage, result any, rerr *rpcError) {
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if rerr != nil {
		out["error"] = rerr
	} else {
		out["result"] = result // nil marshals to null, which is what LSP wants
	}
	body, err := json.Marshal(out)
	if err != nil {
		return
	}
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

func (s *Server) send(msg rpcMessage) {
	msg.JSONRPC = "2.0"
	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

// readMessage reads one LSP frame: headers, blank line, then Content-Length
// bytes of body.
func readMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %q", value)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("frame without Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}
