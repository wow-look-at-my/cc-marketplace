package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.HandlerFunc) *client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &client{api: srv.URL, token: "t", http: srv.Client()}
}

func TestReposReadsTheOrgEndpoint(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/orgs/acme/repos", r.URL.Path)
		assert.Equal(t, "Bearer t", r.Header.Get("Authorization"))
		json.NewEncoder(w).Encode([]repo{{Name: "one", FullName: "acme/one"}})
	})

	got, err := c.repos("acme")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "acme/one", got[0].FullName)
}

func TestReposFallsBackToTheUserEndpoint(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/orgs/") {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		require.Equal(t, "/users/someone/repos", r.URL.Path)
		json.NewEncoder(w).Encode([]repo{{Name: "mine", FullName: "someone/mine"}})
	})

	got, err := c.repos("someone")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "someone/mine", got[0].FullName)
}

func TestReposReportsTheFirstFailureWhenNeitherEndpointAnswers(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	})

	_, err := c.repos("acme")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/orgs/acme/repos")
	assert.Contains(t, err.Error(), "Bad credentials")
}

func TestReposFollowsPages(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var batch []repo
		if page == "1" {
			for i := 0; i < 100; i++ {
				batch = append(batch, repo{FullName: fmt.Sprintf("acme/r%d", i)})
			}
		} else {
			batch = []repo{{FullName: "acme/last"}}
		}
		json.NewEncoder(w).Encode(batch)
	})

	got, err := c.repos("acme")
	require.NoError(t, err)
	require.Len(t, got, 101)
	assert.Equal(t, "acme/last", got[100].FullName)
}

func TestReadmeTextDecodesBase64(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("# Title\n\nProse."))
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/acme/one/readme", r.URL.Path)
		fmt.Fprintf(w, `{"content":%q,"encoding":"base64"}`, body[:10]+"\n"+body[10:])
	})

	assert.Equal(t, "# Title\n\nProse.", c.readmeText("acme/one"))
}

func TestReadmeTextHandlesAMissingOrUndecodableReadme(t *testing.T) {
	missing := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	assert.Empty(t, missing.readmeText("acme/one"))

	garbage := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"content":"!!!not base64!!!","encoding":"base64"}`)
	})
	assert.Empty(t, garbage.readmeText("acme/one"))

	plain := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"content":"raw text","encoding":"none"}`)
	})
	assert.Equal(t, "raw text", plain.readmeText("acme/one"))
}

func TestLoginReadsTheAuthenticatedUser(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/user", r.URL.Path)
		fmt.Fprint(w, `{"login":"someone"}`)
	})
	assert.Equal(t, "someone", c.login())
}

func TestLoginIsEmptyWithoutAuthentication(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Requires authentication"}`, http.StatusUnauthorized)
	})
	assert.Empty(t, c.login())
}

func TestGetReportsMalformedJSON(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{{{`)
	})
	var into []repo
	require.Error(t, c.get("/anything", &into))
}

func TestNewClientHonoursTheEnvironment(t *testing.T) {
	t.Setenv("GITHUB_API_URL", "https://ghe.example.com/api/v3/")
	t.Setenv("GH_TOKEN", "from-env")

	c := newClient()
	assert.Equal(t, "https://ghe.example.com/api/v3", c.api)
	assert.Equal(t, "from-env", c.token)
}

func TestNewClientDefaultsToPublicGitHub(t *testing.T) {
	t.Setenv("GITHUB_API_URL", "")
	assert.Equal(t, defaultAPI, newClient().api)
}

func TestResolveTokenPrefersGHTokenOverGitHubToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "first")
	t.Setenv("GITHUB_TOKEN", "second")
	assert.Equal(t, "first", resolveToken())

	t.Setenv("GH_TOKEN", "")
	assert.Equal(t, "second", resolveToken())
}
