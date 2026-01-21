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

func main() {
	input, _ := io.ReadAll(os.Stdin)
	var hi HookInput
	if err := json.Unmarshal(input, &hi); err != nil {
		fmt.Println("{}")
		return
	}

	if hi.HookEventName != "UserPromptSubmit" || hi.Prompt == "" {
		fmt.Println("{}")
		return
	}

	// Find all @https://* and @git@* URLs
	re := regexp.MustCompile(`@(https://[^\s]+|git@[^\s]+)`)
	matches := re.FindAllStringSubmatch(hi.Prompt, -1)
	if len(matches) == 0 {
		fmt.Println("{}")
		return
	}

	var context strings.Builder
	totalChars := 0

	client := &http.Client{Timeout: 10 * time.Second}

	for _, match := range matches {
		url := match[1]
		fetchURL := normalizeURL(url)

		var content string

		// Check content type first
		resp, err := client.Head(fetchURL)
		if err != nil {
			content = fmt.Sprintf("[Error: Could not connect to %s]", url)
		} else {
			contentType := resp.Header.Get("Content-Type")
			resp.Body.Close()

			if contentType == "" {
				content = fmt.Sprintf("[Error: Could not determine content type for %s]", url)
			} else if !strings.HasPrefix(contentType, "text/") {
				content = fmt.Sprintf("[Error: %s has non-text MIME type: %s]", url, strings.Split(contentType, ";")[0])
			} else {
				// Fetch the content
				resp, err := client.Get(fetchURL)
				if err != nil {
					content = fmt.Sprintf("[Failed to fetch %s]", fetchURL)
				} else {
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					content = string(body)

					// Check if adding this would exceed limit
					if totalChars+len(content) > maxChars {
						remaining := maxChars - totalChars
						if remaining > 1000 {
							content = content[:remaining] + "\n\n... [truncated, exceeded ~50k token limit]"
						} else {
							content = "[Skipped: exceeded ~50k token limit]"
						}
					}
					totalChars += len(content)
				}
			}
		}

		context.WriteString(fmt.Sprintf("\n---\n**Fetched from %s:**\n```\n%s\n```\n", url, content))
	}

	output := HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:     "UserPromptSubmit",
			AdditionalContext: context.String(),
		},
	}
	json.NewEncoder(os.Stdout).Encode(output)
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
