package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// realInstruction reproduces the tail of Claude Code's compaction summarization
// message: the anti-tool preamble plus the "detailed summary" task line. Both
// markers the proxy keys off appear here.
const realInstruction = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

- Tool calls will be REJECTED and will waste your only turn.
- Your entire response must be plain text: an <analysis> block followed by a <summary> block.

Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.`

func compactionBody(model string) []byte {
	b, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 20000,
		"messages": []map[string]any{
			{"role": "user", "content": "please build the feature"},
			{"role": "assistant", "content": "done"},
			{"role": "user", "content": realInstruction},
		},
	})
	return b
}

func normalBody(model string) []byte {
	b, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "user", "content": "what's 2+2?"},
		},
	})
	return b
}

func TestIsCompactionRequest_True(t *testing.T) {
	if !isCompactionRequest(compactionBody("claude-opus-4-8")) {
		t.Fatal("expected compaction request to be detected")
	}
}

func TestIsCompactionRequest_BlockContent(t *testing.T) {
	// content as an array of blocks (the common wire shape) must also match.
	b, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": realInstruction},
			}},
		},
	})
	if !isCompactionRequest(b) {
		t.Fatal("expected block-style content to be detected")
	}
}

func TestIsCompactionRequest_Normal(t *testing.T) {
	if isCompactionRequest(normalBody("claude-opus-4-8")) {
		t.Fatal("normal request must not be detected as compaction")
	}
}

// The marker may legitimately appear in the conversation being summarized -- for
// instance, this very debugging session. It must only trigger when it is the
// FINAL message, never when buried in earlier history.
func TestIsCompactionRequest_MarkerOnlyInEarlierMessage(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": realInstruction}, // quoted earlier
			{"role": "assistant", "content": "got it"},
			{"role": "user", "content": "now add tests"}, // real last turn
		},
	})
	if isCompactionRequest(b) {
		t.Fatal("marker in an earlier message must not trigger detection")
	}
}

func TestIsCompactionRequest_Garbage(t *testing.T) {
	for _, b := range [][]byte{[]byte("not json"), []byte(`{"messages":[]}`), []byte(`{}`)} {
		if isCompactionRequest(b) {
			t.Fatalf("garbage/empty body must not be detected: %s", b)
		}
	}
}

func TestRewriteModel(t *testing.T) {
	out, ok := rewriteModel(compactionBody("claude-opus-4-8"), "claude-haiku-4-5-20251001")
	if !ok {
		t.Fatal("rewrite should succeed on a JSON object")
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("rewritten body is not valid JSON: %v", err)
	}
	if obj["model"] != "claude-haiku-4-5-20251001" {
		t.Fatalf("model not swapped: %v", obj["model"])
	}
	// Other fields must be preserved.
	if obj["max_tokens"].(float64) != 20000 {
		t.Fatalf("max_tokens clobbered: %v", obj["max_tokens"])
	}
	if msgs, ok := obj["messages"].([]any); !ok || len(msgs) != 3 {
		t.Fatalf("messages clobbered: %v", obj["messages"])
	}
}

func TestRewriteModel_NotObject(t *testing.T) {
	if _, ok := rewriteModel([]byte(`["array"]`), "x"); ok {
		t.Fatal("rewrite should report failure on non-object body")
	}
}

func testConfig(t *testing.T, upstream string) *proxyConfig {
	t.Helper()
	u, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("bad upstream: %v", err)
	}
	return &proxyConfig{
		upstream:      u,
		model:         "claude-haiku-4-5-20251001",
		maxInputBytes: defaultMaxInput,
		logger:        log.New(io.Discard, "", 0),
	}
}

// fakeUpstream echoes the model field it received in the X-Got-Model header.
func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var obj map[string]any
		json.Unmarshal(body, &obj)
		model, _ := obj["model"].(string)
		w.Header().Set("X-Got-Model", model)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
}

func gotModel(t *testing.T, cfg *proxyConfig, path string, body []byte) string {
	t.Helper()
	handler := newProxyHandler(cfg)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy returned %d", rec.Code)
	}
	return rec.Header().Get("X-Got-Model")
}

func TestProxy_SwapsOnCompaction(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	if got := gotModel(t, cfg, "/v1/messages", compactionBody("claude-opus-4-8")); got != cfg.model {
		t.Fatalf("compaction request should reach upstream as %s, got %s", cfg.model, got)
	}
}

func TestProxy_LeavesNormalRequest(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	if got := gotModel(t, cfg, "/v1/messages", normalBody("claude-opus-4-8")); got != "claude-opus-4-8" {
		t.Fatalf("normal request model must be untouched, got %s", got)
	}
}

func TestProxy_SizeGuardSkipsSwap(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	cfg.maxInputBytes = 10 // force the body over the guard
	if got := gotModel(t, cfg, "/v1/messages", compactionBody("claude-opus-4-8")); got != "claude-opus-4-8" {
		t.Fatalf("oversized compaction should not be swapped, got %s", got)
	}
}

func TestProxy_DoesNotSwapCountTokens(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	// Same body shape, but the count_tokens sub-route must pass through untouched.
	if got := gotModel(t, cfg, "/v1/messages/count_tokens", compactionBody("claude-opus-4-8")); got != "claude-opus-4-8" {
		t.Fatalf("count_tokens must not be swapped, got %s", got)
	}
}

func TestProxy_ForwardsResponseBody(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	handler := newProxyHandler(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(normalBody("m"))))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "message_stop") {
		t.Fatalf("response body not streamed through: %q", rec.Body.String())
	}
}

func TestExtractText(t *testing.T) {
	if got := extractText(json.RawMessage(`"hello"`)); got != "hello" {
		t.Fatalf("string content: got %q", got)
	}
	got := extractText(json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`))
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Fatalf("block content: got %q", got)
	}
	if extractText(json.RawMessage(`123`)) != "" {
		t.Fatal("numeric content should yield empty string")
	}
}
