package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type HookInput struct {
	HookEventName string `json:"hook_event_name"`
	Prompt        string `json:"prompt"`
}

type HookOutput struct {
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type HookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

const maxChars = 200000 // ~50k tokens

var urlPattern = regexp.MustCompile(`@(https://[^\s]+|git@[^\s]+)`)

// HTTPClient interface for testing
type HTTPClient interface {
	Head(url string) (*http.Response, error)
	Get(url string) (*http.Response, error)
}

func main() {
	output := processInput(os.Stdin, &http.Client{Timeout: 10 * time.Second})
	fmt.Print(output)
}

func processInput(r io.Reader, client HTTPClient) string {
	input, _ := io.ReadAll(r)
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		return "{}\n"
	}

	if hi.HookEventName != "UserPromptSubmit" || hi.Prompt == "" {
		return "{}\n"
	}

	urls := extractURLs(hi.Prompt)
	if len(urls) == 0 {
		return "{}\n"
	}

	context := fetchAllURLs(urls, client)

	output := HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:     "UserPromptSubmit",
			AdditionalContext: context,
		},
	}
	result, _ := json.Marshal(output)
	return string(result) + "\n"
}

func extractURLs(prompt string) []string {
	matches := urlPattern.FindAllStringSubmatch(prompt, -1)
	urls := make([]string, 0, len(matches))
	for _, match := range matches {
		urls = append(urls, match[1])
	}
	return urls
}

func fetchAllURLs(urls []string, client HTTPClient) string {
	var context strings.Builder
	totalChars := 0

	for _, url := range urls {
		content := fetchURL(url, client, &totalChars)
		context.WriteString(fmt.Sprintf("\n---\n**Fetched from %s:**\n```\n%s\n```\n", url, content))
	}

	return context.String()
}

func fetchURL(url string, client HTTPClient, totalChars *int) string {
	fetchURL := normalizeURL(url)

	resp, err := client.Head(fetchURL)
	if err != nil {
		return fmt.Sprintf("[Error: Could not connect to %s]", url)
	}
	contentType := resp.Header.Get("Content-Type")
	resp.Body.Close()

	if contentType == "" {
		return fmt.Sprintf("[Error: Could not determine content type for %s]", url)
	}
	if !strings.HasPrefix(contentType, "text/") {
		return fmt.Sprintf("[Error: %s has non-text MIME type: %s]", url, strings.Split(contentType, ";")[0])
	}

	resp, err = client.Get(fetchURL)
	if err != nil {
		return fmt.Sprintf("[Failed to fetch %s]", fetchURL)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	content := string(body)

	if *totalChars+len(content) > maxChars {
		remaining := maxChars - *totalChars
		if remaining > 1000 {
			content = content[:remaining] + "\n\n... [truncated, exceeded ~50k token limit]"
		} else {
			content = "[Skipped: exceeded ~50k token limit]"
		}
	}
	*totalChars += len(content)

	return content
}

func normalizeURL(url string) string {
	// git@github.com:owner/repo.git -> raw README
	if strings.HasPrefix(url, "git@github.com:") {
		path := strings.TrimPrefix(url, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/README.md", parts[0], parts[1])
		}
	}

	// https://github.com/owner/repo.git -> raw README
	if strings.HasPrefix(url, "https://github.com/") && strings.HasSuffix(url, ".git") {
		path := strings.TrimPrefix(url, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/README.md", parts[0], parts[1])
		}
	}

	// https://github.com/owner/repo/ or https://github.com/owner/repo -> raw README
	if strings.HasPrefix(url, "https://github.com/") {
		path := strings.TrimPrefix(url, "https://github.com/")
		path = strings.TrimSuffix(path, "/")
		parts := strings.Split(path, "/")
		if len(parts) == 2 {
			return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/README.md", parts[0], parts[1])
		}
	}

	return url
}
