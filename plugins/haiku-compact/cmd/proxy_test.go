package main

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
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
	require.True(t, isCompactionRequest(compactionBody("claude-opus-4-8")))

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
	require.True(t, isCompactionRequest(b))

}

func TestIsCompactionRequest_Normal(t *testing.T) {
	require.False(t, isCompactionRequest(normalBody("claude-opus-4-8")))

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
	require.False(t, isCompactionRequest(b))

}

func TestIsCompactionRequest_Garbage(t *testing.T) {
	for _, b := range [][]byte{[]byte("not json"), []byte(`{"messages":[]}`), []byte(`{}`)} {
		require.False(t, isCompactionRequest(b))

	}
}

func TestRewriteModel(t *testing.T) {
	out, ok := rewriteModel(compactionBody("claude-opus-4-8"), "claude-haiku-4-5-20251001")
	require.True(t, ok)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(out, &obj))

	require.Equal(t, "claude-haiku-4-5-20251001", obj["model"])

	// Other fields must be preserved.
	require.Equal(t, float64(20000), obj["max_tokens"].(float64))

	msgs, ok := obj["messages"].([]any)
	require.False(t, !ok || len(msgs) != 3)

}

func TestRewriteModel_NotObject(t *testing.T) {
	_, ok := rewriteModel([]byte(`["array"]`), "x")
	require.False(t, ok)

}

func testConfig(t *testing.T, upstream string) *proxyConfig {
	t.Helper()
	u, err := url.Parse(upstream)
	require.Nil(t, err)

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
	require.Equal(t, http.StatusOK, rec.Code)

	return rec.Header().Get("X-Got-Model")
}

func TestProxy_SwapsOnCompaction(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	got := gotModel(t, cfg, "/v1/messages", compactionBody("claude-opus-4-8"))
	require.Equal(t, cfg.model, got)

}

func TestProxy_LeavesNormalRequest(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	got := gotModel(t, cfg, "/v1/messages", normalBody("claude-opus-4-8"))
	require.Equal(t, "claude-opus-4-8", got)

}

func TestProxy_SizeGuardSkipsSwap(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	cfg.maxInputBytes = 10 // force the body over the guard
	got := gotModel(t, cfg, "/v1/messages", compactionBody("claude-opus-4-8"))
	require.Equal(t, "claude-opus-4-8", got)

}

func TestProxy_DoesNotSwapCountTokens(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	// Same body shape, but the count_tokens sub-route must pass through untouched.
	got := gotModel(t, cfg, "/v1/messages/count_tokens", compactionBody("claude-opus-4-8"))
	require.Equal(t, "claude-opus-4-8", got)

}

func TestProxy_ForwardsResponseBody(t *testing.T) {
	up := fakeUpstream(t)
	defer up.Close()
	cfg := testConfig(t, up.URL)
	handler := newProxyHandler(cfg)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(normalBody("m"))))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Contains(t, rec.Body.String(), "message_stop")

}

func TestExtractText(t *testing.T) {
	got := extractText(json.RawMessage(`"hello"`))
	require.Equal(t, "hello", got)

	got = extractText(json.RawMessage(`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`))
	require.False(t, !strings.Contains(got, "a") || !strings.Contains(got, "b"))

	require.Equal(t, "", extractText(json.RawMessage(`123`)))

}
