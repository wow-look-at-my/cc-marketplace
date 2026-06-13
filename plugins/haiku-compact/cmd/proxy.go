package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

// compactionMarker uniquely identifies Claude Code's context-compaction request.
// Both prompt variants -- "...the conversation so far" and "...the RECENT portion
// of the conversation" -- open the summarization instruction with this exact
// phrase, and that instruction is the FINAL message of the request. (Source:
// claude-code cli.js, summarization prompt builder.)
const compactionMarker = "Your task is to create a detailed summary of the"

// secondaryMarker is a second phrase from the same instruction. Requiring both
// makes an accidental match on an ordinary user turn effectively impossible.
const secondaryMarker = "Respond with TEXT ONLY"

// proxyConfig holds the runtime configuration for the intercepting proxy.
type proxyConfig struct {
	upstream      *url.URL    // real Anthropic endpoint to forward to
	model         string      // model id to substitute on compaction requests
	maxInputBytes int64       // skip the swap if the request body exceeds this (Haiku context guard); 0 disables the guard
	logger        *log.Logger // minimal logging; never logs bodies or headers
}

// extractText returns the concatenated text of an Anthropic message `content`
// field, which may be either a plain string or an array of content blocks.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, bl := range blocks {
			if bl.Text != "" {
				b.WriteString(bl.Text)
				b.WriteByte('\n')
			}
		}
		return b.String()
	}
	return ""
}

// isCompactionRequest reports whether body is Claude Code's compaction
// summarization call. It inspects only the LAST message: the summarization
// instruction is always appended last, so checking it (rather than scanning the
// whole body) avoids false positives when the conversation being summarized
// merely quotes the marker -- including the conversation in which a user is
// debugging this very proxy.
func isCompactionRequest(body []byte) bool {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Messages) == 0 {
		return false
	}
	text := extractText(req.Messages[len(req.Messages)-1].Content)
	return strings.Contains(text, compactionMarker) && strings.Contains(text, secondaryMarker)
}

// rewriteModel returns body with its top-level "model" field replaced by model.
// All other fields are preserved byte-for-byte via json.RawMessage. The bool is
// false (and the original body returned) if the body is not a JSON object.
func rewriteModel(body []byte, model string) ([]byte, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, false
	}
	m, err := json.Marshal(model)
	if err != nil {
		return body, false
	}
	obj["model"] = m
	out, err := json.Marshal(obj)
	if err != nil {
		return body, false
	}
	return out, true
}

// isMessagesCreate reports whether the request is a POST to the messages-create
// endpoint (and not the /count_tokens sub-route).
func isMessagesCreate(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/messages")
}

// maybeSwap reads the request body and, if it is a compaction request within the
// size guard, rewrites the model to the configured Haiku model. The body is
// always restored on r so the request can be forwarded regardless of outcome.
// It is fail-open: any error leaves the request untouched.
func maybeSwap(r *http.Request, cfg *proxyConfig) {
	if !isMessagesCreate(r) || r.Body == nil {
		return
	}
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		// Body partially consumed; forward what we have rather than break the call.
		restoreBody(r, body)
		return
	}
	out := body
	if cfg.maxInputBytes > 0 && int64(len(body)) > cfg.maxInputBytes {
		cfg.logger.Printf("skipping swap: request %d bytes exceeds context guard of %d (likely too large for %s)",
			len(body), cfg.maxInputBytes, cfg.model)
	} else if isCompactionRequest(body) {
		if rewritten, ok := rewriteModel(body, cfg.model); ok {
			out = rewritten
			cfg.logger.Printf("compaction detected: swapped model to %s (%d bytes)", cfg.model, len(body))
		}
	}
	restoreBody(r, out)
}

// restoreBody sets b as the request body and fixes the length fields so the
// outgoing request is well-formed.
func restoreBody(r *http.Request, b []byte) {
	r.Body = io.NopCloser(bytes.NewReader(b))
	r.ContentLength = int64(len(b))
	r.Header.Set("Content-Length", strconv.Itoa(len(b)))
}

// newProxyHandler builds the reverse-proxy handler. Requests are inspected and
// possibly rewritten by maybeSwap, then forwarded to cfg.upstream with all
// original headers (auth included) intact. Responses stream straight through,
// so Server-Sent Events from the API are flushed to Claude Code immediately.
func newProxyHandler(cfg *proxyConfig) http.Handler {
	rp := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = cfg.upstream.Scheme
			r.URL.Host = cfg.upstream.Host
			r.Host = cfg.upstream.Host
			r.URL.Path = singleJoiningSlash(cfg.upstream.Path, r.URL.Path)
		},
		FlushInterval: -1, // flush each chunk immediately for streaming responses
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			cfg.logger.Printf("upstream error: %v", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		maybeSwap(r, cfg)
		rp.ServeHTTP(w, r)
	})
}

// singleJoiningSlash joins two URL path segments with exactly one slash.
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		if a == "" {
			return b
		}
		return a + "/" + b
	}
	return a + b
}
