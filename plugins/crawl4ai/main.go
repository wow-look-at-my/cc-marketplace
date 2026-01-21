package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var crawl4aiURL = "http://localhost:11235/crawl"

// Crawl4AI types
type CrawlRequest struct {
	URLs          []string `json:"urls"`
	CrawlerConfig any      `json:"crawler_config,omitempty"`
}

type Link struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

type Links struct {
	Internal []Link `json:"internal"`
	External []Link `json:"external"`
}

type Markdown struct {
	RawMarkdown string `json:"raw_markdown"`
}

type CrawlResultItem struct {
	URL      string         `json:"url"`
	Success  bool           `json:"success"`
	Markdown Markdown       `json:"markdown"`
	Links    Links          `json:"links"`
	Metadata map[string]any `json:"metadata"`
}

type CrawlResponse struct {
	Success bool              `json:"success"`
	Results []CrawlResultItem `json:"results"`
}

// Tool input/output types
type CrawlArgs struct {
	URL string `json:"url" jsonschema:"The URL to crawl"`
}

type CrawlOutput struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
	Links   []Link `json:"links"`
}

func crawl(ctx context.Context, req *mcp.CallToolRequest, args CrawlArgs) (*mcp.CallToolResult, any, error) {
	reqBody := CrawlRequest{
		URLs: []string{args.URL},
		CrawlerConfig: map[string]any{
			"type": "CrawlerRunConfig",
			"params": map[string]any{
				"cache_mode": map[string]any{
					"type":   "CacheMode",
					"params": "enabled",
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, err
	}

	resp, err := http.Post(crawl4aiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return errorResult(fmt.Sprintf("failed to connect to crawl4ai: %v", err)), nil, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	var crawlResp CrawlResponse
	if err := json.Unmarshal(respBody, &crawlResp); err != nil {
		return errorResult(fmt.Sprintf("failed to parse response: %v", err)), nil, nil
	}

	if len(crawlResp.Results) == 0 {
		return errorResult("no results returned"), nil, nil
	}

	result := crawlResp.Results[0]
	if !result.Success {
		return errorResult("crawl failed"), nil, nil
	}

	out := CrawlOutput{
		URL:     result.URL,
		Content: result.Markdown.RawMarkdown,
		Links:   result.Links.Internal,
	}
	if title, ok := result.Metadata["title"].(string); ok {
		out.Title = title
	}

	outJSON, _ := json.MarshalIndent(out, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(outJSON)}},
	}, nil, nil
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

func main() {
	if url := os.Getenv("CRAWL4AI_URL"); url != "" {
		crawl4aiURL = url
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "crawl4ai",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "crawl",
		Description: "Crawl a URL and return its content as markdown plus all links found on the page. Use this to explore documentation sites - read the content, then decide which links to follow.",
	}, crawl)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
