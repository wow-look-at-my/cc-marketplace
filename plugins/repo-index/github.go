package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultAPI = "https://api.github.com"

// apiBase picks the host this process talks to when it cannot go through gh.
// GH_HOST names a GitHub state mirror, and every gh call in this environment
// already rides it. A direct request that ignores it spends real GitHub API
// quota for an answer the mirror holds. gh builds "https://<host>/api/v3" for a
// non-dotcom host, so this builds the same base.
func apiBase() string {
	if api := os.Getenv("GITHUB_API_URL"); api != "" {
		return strings.TrimSuffix(api, "/")
	}
	host := strings.TrimPrefix(strings.TrimPrefix(os.Getenv("GH_HOST"), "https://"), "http://")
	host = strings.Trim(host, "/")
	if host == "" || host == "github.com" {
		return defaultAPI
	}
	return "https://" + host + "/api/v3"
}

// repo is the part of a GitHub repository this plugin reads.
type repo struct {
	Name        string   `json:"name"`
	FullName    string   `json:"full_name"`
	HTMLURL     string   `json:"html_url"`
	Description string   `json:"description"`
	Topics      []string `json:"topics"`
	Archived    bool     `json:"archived"`
	Fork        bool     `json:"fork"`
}

type client struct {
	api   string
	token string
	http  *http.Client
	// viaGH sends the request through the gh CLI instead of this process. The
	// CLI already holds the user's credential and knows their host, and some
	// networks reach GitHub only through it.
	viaGH bool
}

func newClient() *client {
	c := &client{
		api:   apiBase(),
		token: resolveToken(),
		http:  &http.Client{Timeout: 30 * time.Second},
	}
	// gh resolves its own host from GH_HOST, so it is preferred whenever the
	// caller has not pinned a base of their own.
	if _, err := exec.LookPath("gh"); err == nil && os.Getenv("GITHUB_API_URL") == "" {
		c.viaGH = true
	}
	return c
}

// ghGet runs `gh api <path>` and returns the body.
func (c *client) ghGet(path string) ([]byte, error) {
	cmd := exec.Command("gh", "api", strings.TrimPrefix(path, "/"))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// resolveToken prefers the environment, then the gh CLI. An empty token still
// works against public repositories, at a lower rate limit.
func resolveToken() string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *client) get(path string, into any) error {
	if c.viaGH {
		body, err := c.ghGet(path)
		if err != nil {
			return err
		}
		return json.Unmarshal(body, into)
	}
	req, err := http.NewRequest(http.MethodGet, c.api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, into)
}

// login returns the authenticated user. An empty result is not an error: an
// unauthenticated run has no user, and owner discovery has other sources.
func (c *client) login() string {
	var user struct {
		Login string `json:"login"`
	}
	if err := c.get("/user", &user); err != nil {
		return ""
	}
	return user.Login
}

// repos lists an owner's repositories. It tries the org endpoint first and
// falls back to the user endpoint, because the caller cannot know which an
// owner is without another request.
func (c *client) repos(owner string) ([]repo, error) {
	orgErr := error(nil)
	for _, shape := range []string{"/orgs/%s/repos", "/users/%s/repos"} {
		list, err := c.pagedRepos(fmt.Sprintf(shape, url.PathEscape(owner)))
		if err == nil {
			return list, nil
		}
		if orgErr == nil {
			orgErr = err
		}
	}
	return nil, orgErr
}

const maxPages = 10

func (c *client) pagedRepos(path string) ([]repo, error) {
	var all []repo
	for page := 1; page <= maxPages; page++ {
		var batch []repo
		if err := c.get(fmt.Sprintf("%s?per_page=100&sort=pushed&page=%d", path, page), &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			return all, nil
		}
	}
	return all, nil
}

// readmeText fetches and decodes a repository's README. An empty string means
// the repository has none.
func (c *client) readmeText(fullName string) string {
	var body struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.get("/repos/"+fullName+"/readme", &body); err != nil {
		return ""
	}
	if body.Encoding != "base64" {
		return body.Content
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body.Content, "\n", ""))
	if err != nil {
		return ""
	}
	return string(raw)
}
