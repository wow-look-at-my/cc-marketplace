package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
)

// Mock HTTP client for testing
type mockClient struct {
	headFunc	func(url string) (*http.Response, error)
	getFunc		func(url string) (*http.Response, error)
}

func (m *mockClient) Head(url string) (*http.Response, error) {
	return m.headFunc(url)
}

func (m *mockClient) Get(url string) (*http.Response, error) {
	return m.getFunc(url)
}

func mockResponse(body string, contentType string) *http.Response {
	return &http.Response{
		Body:	io.NopCloser(strings.NewReader(body)),
		Header:	http.Header{"Content-Type": []string{contentType}},
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name		string
		input		string
		expected	string
	}{
		{
			name:		"git ssh url",
			input:		"git@github.com:owner/repo.git",
			expected:	"https://raw.githubusercontent.com/owner/repo/HEAD/README.md",
		},
		{
			name:		"https github url with .git",
			input:		"https://github.com/owner/repo.git",
			expected:	"https://raw.githubusercontent.com/owner/repo/HEAD/README.md",
		},
		{
			name:		"https github url without .git",
			input:		"https://github.com/owner/repo",
			expected:	"https://raw.githubusercontent.com/owner/repo/HEAD/README.md",
		},
		{
			name:		"https github url with trailing slash",
			input:		"https://github.com/owner/repo/",
			expected:	"https://raw.githubusercontent.com/owner/repo/HEAD/README.md",
		},
		{
			name:		"regular https url passthrough",
			input:		"https://example.com/file.txt",
			expected:	"https://example.com/file.txt",
		},
		{
			name:		"github url with subpath passthrough",
			input:		"https://github.com/owner/repo/blob/main/file.txt",
			expected:	"https://github.com/owner/repo/blob/main/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeURL(tt.input)
			assert.Equal(t, tt.expected, result, "normalizeURL(%q)", tt.input)
		})
	}
}

func TestExtractURLs(t *testing.T) {
	tests := []struct {
		name		string
		prompt		string
		expected	[]string
	}{
		{
			name:		"no urls",
			prompt:		"hello world",
			expected:	[]string{},
		},
		{
			name:		"single https url",
			prompt:		"check @https://example.com/file.txt please",
			expected:	[]string{"https://example.com/file.txt"},
		},
		{
			name:		"multiple urls",
			prompt:		"compare @https://a.com and @https://b.com",
			expected:	[]string{"https://a.com", "https://b.com"},
		},
		{
			name:		"git ssh url",
			prompt:		"look at @git@github.com:owner/repo.git",
			expected:	[]string{"git@github.com:owner/repo.git"},
		},
		{
			name:		"url without @ prefix ignored",
			prompt:		"visit https://example.com",
			expected:	[]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractURLs(tt.prompt)
			assert.Equal(t, len(tt.expected), len(result), "extractURLs(%q) url count", tt.prompt)
			for i, url := range result {
				assert.Equal(t, tt.expected[i], url, "extractURLs(%q)[%d]", tt.prompt, i)
			}
		})
	}
}

func TestFetchURL(t *testing.T) {
	tests := []struct {
		name		string
		url		string
		headResp	*http.Response
		headErr		error
		getResp		*http.Response
		getErr		error
		wantContain	string
	}{
		{
			name:		"connection error",
			url:		"https://example.com",
			headErr:	errors.New("connection refused"),
			wantContain:	"[Error: Could not connect to",
		},
		{
			name:		"empty content type",
			url:		"https://example.com",
			headResp:	mockResponse("", ""),
			wantContain:	"[Error: Could not determine content type",
		},
		{
			name:		"non-text content type",
			url:		"https://example.com/image.png",
			headResp:	mockResponse("", "image/png"),
			wantContain:	"non-text MIME type: image/png",
		},
		{
			name:		"successful fetch",
			url:		"https://example.com/file.txt",
			headResp:	mockResponse("", "text/plain"),
			getResp:	mockResponse("hello world", "text/plain"),
			wantContain:	"hello world",
		},
		{
			name:		"get error after successful head",
			url:		"https://example.com/file.txt",
			headResp:	mockResponse("", "text/plain"),
			getErr:		errors.New("timeout"),
			wantContain:	"[Failed to fetch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockClient{
				headFunc: func(url string) (*http.Response, error) {
					return tt.headResp, tt.headErr
				},
				getFunc: func(url string) (*http.Response, error) {
					return tt.getResp, tt.getErr
				},
			}
			totalChars := 0
			result := fetchURL(tt.url, client, &totalChars)
			assert.Contains(t, result, tt.wantContain, "fetchURL(%q)", tt.url)
		})
	}
}

func TestFetchURLTruncation(t *testing.T) {
	largeContent := strings.Repeat("x", maxChars+1000)
	client := &mockClient{
		headFunc: func(url string) (*http.Response, error) {
			return mockResponse("", "text/plain"), nil
		},
		getFunc: func(url string) (*http.Response, error) {
			return mockResponse(largeContent, "text/plain"), nil
		},
	}

	totalChars := 0
	result := fetchURL("https://example.com", client, &totalChars)
	assert.Contains(t, result, "truncated")
}

func TestFetchURLSkipWhenNearLimit(t *testing.T) {
	// Content larger than remaining space (1500 chars when only 500 remain)
	largeContent := strings.Repeat("x", 1500)
	client := &mockClient{
		headFunc: func(url string) (*http.Response, error) {
			return mockResponse("", "text/plain"), nil
		},
		getFunc: func(url string) (*http.Response, error) {
			return mockResponse(largeContent, "text/plain"), nil
		},
	}

	totalChars := maxChars - 500	// only 500 chars remaining
	result := fetchURL("https://example.com", client, &totalChars)
	assert.Contains(t, result, "[Skipped: exceeded")
}

func TestProcessInput(t *testing.T) {
	tests := []struct {
		name		string
		input		string
		wantContain	string
	}{
		{
			name:		"invalid json",
			input:		"not json",
			wantContain:	"{}",
		},
		{
			name:		"wrong event type",
			input:		`{"hook_event_name":"PreToolUse","prompt":"@https://example.com"}`,
			wantContain:	"{}",
		},
		{
			name:		"empty prompt",
			input:		`{"hook_event_name":"UserPromptSubmit","prompt":""}`,
			wantContain:	"{}",
		},
		{
			name:		"no urls in prompt",
			input:		`{"hook_event_name":"UserPromptSubmit","prompt":"hello world"}`,
			wantContain:	"{}",
		},
		{
			name:		"valid prompt with url",
			input:		`{"hook_event_name":"UserPromptSubmit","prompt":"check @https://example.com"}`,
			wantContain:	"additionalContext",
		},
	}

	client := &mockClient{
		headFunc: func(url string) (*http.Response, error) {
			return mockResponse("", "text/plain"), nil
		},
		getFunc: func(url string) (*http.Response, error) {
			return mockResponse("test content", "text/plain"), nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processInput(strings.NewReader(tt.input), client)
			assert.Contains(t, result, tt.wantContain)
		})
	}
}

func TestFetchAllURLs(t *testing.T) {
	client := &mockClient{
		headFunc: func(url string) (*http.Response, error) {
			return mockResponse("", "text/plain"), nil
		},
		getFunc: func(url string) (*http.Response, error) {
			return mockResponse("content for "+url, "text/plain"), nil
		},
	}

	urls := []string{"https://a.com", "https://b.com"}
	result := fetchAllURLs(urls, client)

	assert.Contains(t, result, "Fetched from https://a.com")
	assert.Contains(t, result, "Fetched from https://b.com")
	assert.Contains(t, result, "content for https://a.com")
}
