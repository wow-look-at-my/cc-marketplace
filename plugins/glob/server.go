// server.go implements the MCP protocol layer: newline-delimited JSON-RPC
// 2.0 over stdio, exactly as claude-code frames it (one JSON object per
// line, trailing \r tolerated, all logging on stderr — never stdout).
//
// This file is tool-agnostic glue. A sibling plugin (e.g. grep) should be
// able to copy it verbatim and only swap the mcpTool implementation wired
// up in main.go.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// defaultProtocolVersion is used when the client does not send one.
// claude-code 2.1.207 sends "2025-11-25" and accepts it back.
const defaultProtocolVersion = "2025-11-25"

// JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// mcpTool is the contract between the protocol glue and a tool
// implementation. Call returns either a tool-level result (operational
// failures use IsError:true) or an *rpcError for JSON-RPC-level problems
// (malformed arguments).
type mcpTool interface {
	Name() string
	ListEntry() toolListEntry
	Call(args json.RawMessage) (*toolResult, *rpcError)
}

type toolResult struct {
	Text    string
	IsError bool
}

type toolAnnotations struct {
	ReadOnlyHint bool `json:"readOnlyHint"`
}

// toolListEntry is one element of the tools/list response. InputSchema is
// raw JSON so the property order the model sees matches the builtin
// byte-for-byte (Go maps would alphabetize it).
type toolListEntry struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema json.RawMessage  `json:"inputSchema"`
	Annotations *toolAnnotations `json:"annotations,omitempty"`
	Meta        map[string]any   `json:"_meta,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
}

type capabilities struct {
	Tools struct{} `json:"tools"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsListResult struct {
	Tools []toolListEntry `json:"tools"`
}

type callToolResultJSON struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type server struct {
	in   io.Reader
	out  io.Writer
	logf func(format string, args ...any)

	name    string
	version string
	tools   []mcpTool
	gateEnv string // env var consulted by the version gate (CC_<NAME>_PLUGIN)

	expose        bool
	clientName    string
	clientVersion string
}

func newServer(in io.Reader, out io.Writer, logf func(string, ...any), name string, tools []mcpTool, gateEnv string) *server {
	return &server{
		in:      in,
		out:     out,
		logf:    logf,
		name:    name,
		version: "1.0.0",
		tools:   tools,
		gateEnv: gateEnv,
		// Before initialize we have no clientInfo; the gate treats an
		// unknown client as "expose" (env overrides still apply).
		expose: gateAllows(os.Getenv(gateEnv), "", ""),
	}
}

// run processes requests sequentially until stdin reaches EOF (clean
// shutdown, returns nil) or a read error occurs.
func (s *server) run() error {
	r := bufio.NewReaderSize(s.in, 64*1024)
	for {
		line, err := r.ReadString('\n')
		if trimmed := strings.TrimRight(line, "\r\n"); trimmed != "" {
			s.handleLine(trimmed)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (s *server) handleLine(line string) {
	data := []byte(line)
	var req rpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		code, msg := codeParseError, "Parse error"
		if json.Valid(data) {
			// Valid JSON that is not a request object (e.g. a batch
			// array — MCP dropped JSON-RPC batching).
			code, msg = codeInvalidRequest, "Invalid Request"
		}
		s.reply(&rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: code, Message: msg}})
		return
	}
	if len(req.ID) == 0 {
		// Notification: tolerate every method, known or unknown
		// (notifications/initialized, notifications/cancelled, ...).
		return
	}
	switch req.Method {
	case "initialize":
		s.handleInitialize(&req)
	case "ping":
		s.replyResult(req.ID, struct{}{})
	case "tools/list":
		s.handleToolsList(&req)
	case "tools/call":
		s.handleToolsCall(&req)
	default:
		s.replyError(req.ID, codeMethodNotFound, "Method not found: "+req.Method)
	}
}

func (s *server) handleInitialize(req *rpcRequest) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	// Tolerate absent or malformed params: everything stays zero-valued
	// and the gate falls back to its unknown-client behavior.
	_ = json.Unmarshal(req.Params, &params)
	s.clientName = params.ClientInfo.Name
	s.clientVersion = params.ClientInfo.Version
	mode := os.Getenv(s.gateEnv)
	s.expose = gateAllows(mode, s.clientName, s.clientVersion)
	s.logf("client=%q version=%q %s=%q -> expose=%v", s.clientName, s.clientVersion, s.gateEnv, mode, s.expose)

	pv := params.ProtocolVersion
	if pv == "" {
		pv = defaultProtocolVersion
	}
	s.replyResult(req.ID, initializeResult{
		ProtocolVersion: pv,
		ServerInfo:      serverInfo{Name: s.name, Version: s.version},
	})
}

func (s *server) handleToolsList(req *rpcRequest) {
	entries := make([]toolListEntry, 0, len(s.tools))
	if s.expose {
		for _, t := range s.tools {
			entries = append(entries, t.ListEntry())
		}
	}
	s.replyResult(req.ID, toolsListResult{Tools: entries})
}

func (s *server) handleToolsCall(req *rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		s.replyError(req.ID, codeInvalidParams, "tools/call params must include a tool name")
		return
	}
	var target mcpTool
	if s.expose {
		for _, t := range s.tools {
			if t.Name() == params.Name {
				target = t
				break
			}
		}
	}
	if target == nil {
		s.replyError(req.ID, codeInvalidParams, fmt.Sprintf("Unknown tool: %s", params.Name))
		return
	}
	res, rpcErr := target.Call(params.Arguments)
	if rpcErr != nil {
		s.reply(&rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
		return
	}
	s.replyResult(req.ID, callToolResultJSON{
		Content: []contentBlock{{Type: "text", Text: res.Text}},
		IsError: res.IsError,
	})
}

func (s *server) replyResult(id json.RawMessage, result any) {
	s.reply(&rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *server) replyError(id json.RawMessage, code int, msg string) {
	s.reply(&rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *server) reply(resp *rpcResponse) {
	b, err := json.Marshal(resp)
	if err != nil {
		s.logf("marshal response: %v", err)
		return
	}
	b = append(b, '\n')
	if _, err := s.out.Write(b); err != nil {
		s.logf("write response: %v", err)
	}
}
