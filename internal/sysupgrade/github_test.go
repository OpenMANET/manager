package sysupgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleReleasesJSON = `[
  {
    "tag_name": "v1.8.0",
    "name": "1.8.0",
    "body": "release notes",
    "draft": false,
    "prerelease": false,
    "published_at": "2026-04-20T12:00:00Z",
    "assets": [
      {"name": "openmanet-1.8.0.img.gz", "browser_download_url": "https://example.com/img.gz", "size": 12345, "content_type": "application/gzip"},
      {"name": "sha256sums", "browser_download_url": "https://example.com/sha256sums", "size": 200, "content_type": "text/plain"}
    ]
  },
  {
    "tag_name": "v1.7.0",
    "name": "1.7.0",
    "body": "older release",
    "draft": false,
    "prerelease": false,
    "published_at": "2026-03-10T12:00:00Z",
    "assets": []
  },
  {
    "tag_name": "draft-tag",
    "name": "draft",
    "draft": true,
    "prerelease": false,
    "published_at": "2026-04-25T12:00:00Z",
    "assets": []
  }
]`

func TestGitHubReleasesClient_FetchReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/OpenMANET/firmware/releases", r.URL.Path)
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		assert.NotEmpty(t, r.Header.Get("User-Agent"))

		_, _ = w.Write([]byte(sampleReleasesJSON))
	}))
	t.Cleanup(srv.Close)

	c := &GitHubReleasesClient{
		HTTP:    srv.Client(),
		Repo:    "OpenMANET/firmware",
		BaseURL: srv.URL,
		Log:     zerolog.Nop(),
	}

	rels, err := c.FetchReleases(context.Background())
	require.NoError(t, err)

	// Drafts filtered.
	require.Len(t, rels, 2)
	assert.Equal(t, "v1.8.0", rels[0].Tag)
	assert.Equal(t, "1.8.0", rels[0].Version)
	assert.Equal(t, "v1.7.0", rels[1].Tag)
	require.Len(t, rels[0].Assets, 2)
	assert.Equal(t, "openmanet-1.8.0.img.gz", rels[0].Assets[0].Name)
	assert.Equal(t, int64(12345), rels[0].Assets[0].SizeBytes)
}

func TestGitHubReleasesClient_RateLimitWarning(t *testing.T) {
	logged := false
	logger := zerolog.New(zerolog.NewTestWriter(t)).Hook(zerolog.HookFunc(func(e *zerolog.Event, l zerolog.Level, _ string) {
		if l == zerolog.WarnLevel {
			logged = true
		}

		_ = e
	}))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "5")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	c := &GitHubReleasesClient{
		HTTP:    srv.Client(),
		Repo:    "OpenMANET/firmware",
		BaseURL: srv.URL,
		Log:     logger,
	}

	_, err := c.FetchReleases(context.Background())
	require.NoError(t, err)
	assert.True(t, logged, "expected a rate-limit warn log")
}

func TestGitHubReleasesClient_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "rate limited"}`))
	}))
	t.Cleanup(srv.Close)

	c := &GitHubReleasesClient{
		HTTP:    srv.Client(),
		Repo:    "OpenMANET/firmware",
		BaseURL: srv.URL,
		Log:     zerolog.Nop(),
	}

	_, err := c.FetchReleases(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
