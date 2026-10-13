// Package rgmcp is the MCP stdio server, version gate, ripgrep runner,
// output persistence and path collator that the glob and grep plugins share.
// A plugin supplies only its Tool and the name of its gate variable.
package rgmcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const DefaultProtocolVersion = "2025-11-25"

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Tool is the contract between the protocol glue and a tool implementation.
type Tool interface {
	Name() string
	ListEntry() ToolListEntry
	Call(args json.RawMessage) (*ToolResult, *RPCError)
}

type ToolResult struct {
	Text    string
	IsError bool
}

type ToolAnnotations struct {
	ReadOnlyHint bool `json:"readOnlyHint"`
}

// ToolListEntry is a single element of the tools/list response.
// InputSchema is raw JSON so the property order the model sees matches
// the builtin byte-for-byte (Go maps would alphabetize it).
type ToolListEntry struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema json.RawMessage  `json:"inputSchema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
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
	Tools []ToolListEntry `json:"tools"`
}

type callToolResultJSON struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Server struct {
	in   io.Reader
	out  io.Writer
	logf func(format string, args ...any)

	name    string
	version string
	tools   []Tool
	gateEnv string // env var consulted by the version gate (CC_<NAME>_PLUGIN)

	expose        bool
	clientName    string
	clientVersion string
}

func NewServer(in io.Reader, out io.Writer, logf func(string, ...any), name string, tools []Tool, gateEnv string) *Server {
	return &Server{
		in:      in,
		out:     out,
		logf:    logf,
		name:    name,
		version: "1",
		tools:   tools,
		gateEnv: gateEnv,
		// Before initialize we have no clientInfo; the gate treats an unknown client as "expose" (env overrides still apply).
		expose: gateAllows(os.Getenv(gateEnv), "", ""),
	}
}

// Run processes requests sequentially until stdin reaches EOF (clean
// shutdown, returns nil) or a read error occurs.
func (s *Server) Run() error {
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

func (s *Server) handleLine(line string) {
	data := []byte(line)
	var req rpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		code, msg := CodeParseError, "Parse error"
		if json.Valid(data) {
			// Valid JSON that is not a request object (e.g. a batch array — MCP dropped JSON-RPC batching).
			code, msg = CodeInvalidRequest, "Invalid Request"
		}
		s.reply(&rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &RPCError{Code: code, Message: msg}})
		return
	}
	if len(req.ID) == 0 {
		// Notification: tolerate every method, known or unknown (notifications/initialized, notifications/cancelled, ...).
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
		s.replyError(req.ID, CodeMethodNotFound, "Method not found: "+req.Method)
	}
}

func (s *Server) handleInitialize(req *rpcRequest) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	_ = json.Unmarshal(req.Params, &params)
	s.clientName = params.ClientInfo.Name
	s.clientVersion = params.ClientInfo.Version
	mode := os.Getenv(s.gateEnv)
	s.expose = gateAllows(mode, s.clientName, s.clientVersion)
	s.logf("client=%q version=%q %s=%q -> expose=%v", s.clientName, s.clientVersion, s.gateEnv, mode, s.expose)

	pv := params.ProtocolVersion
	if pv == "" {
		pv = DefaultProtocolVersion
	}
	s.replyResult(req.ID, initializeResult{
		ProtocolVersion: pv,
		ServerInfo:      serverInfo{Name: s.name, Version: s.version},
	})
}

func (s *Server) handleToolsList(req *rpcRequest) {
	entries := make([]ToolListEntry, 0, len(s.tools))
	if s.expose {
		for _, t := range s.tools {
			entries = append(entries, t.ListEntry())
		}
	}
	s.replyResult(req.ID, toolsListResult{Tools: entries})
}

func (s *Server) handleToolsCall(req *rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		s.replyError(req.ID, CodeInvalidParams, "tools/call params must include a tool name")
		return
	}
	var target Tool
	if s.expose {
		for _, t := range s.tools {
			if t.Name() == params.Name {
				target = t
				break
			}
		}
	}
	if target == nil {
		s.replyError(req.ID, CodeInvalidParams, fmt.Sprintf("Unknown tool: %s", params.Name))
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

func (s *Server) replyResult(id json.RawMessage, result any) {
	s.reply(&rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) replyError(id json.RawMessage, code int, msg string) {
	s.reply(&rpcResponse{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg}})
}

func (s *Server) reply(resp *rpcResponse) {
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
