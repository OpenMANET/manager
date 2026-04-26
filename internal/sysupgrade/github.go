package sysupgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"
)

// githubAPIBase is the GitHub REST API base URL used by GitHubReleasesClient.
const githubAPIBase = "https://api.github.com"

// githubReleasesBodyLimit caps the size of a /repos/.../releases response
// the client will read into memory. Real responses for ~30 releases are
// under ~200 KiB; 4 MiB is a safe ceiling that bounds memory while
// accommodating large release-note bodies.
const githubReleasesBodyLimit = 4 << 20

// userAgent is the User-Agent header sent on every GitHub request.
// GitHub rejects requests without one.
const userAgent = "openmanetd/sysupgrade"

// GitHubReleasesClient fetches release metadata from the GitHub REST API.
// All fields are optional; sensible defaults are applied at first use.
type GitHubReleasesClient struct {
	Log      zerolog.Logger
	HTTP     *http.Client
	Repo     string
	BaseURL  string
	PerPage  int
	MaxPages int
}

func (c *GitHubReleasesClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}

	return &http.Client{Timeout: 30 * time.Second}
}

func (c *GitHubReleasesClient) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}

	return githubAPIBase
}

func (c *GitHubReleasesClient) perPage() int {
	if c.PerPage > 0 {
		return c.PerPage
	}

	return 30
}

func (c *GitHubReleasesClient) maxPages() int {
	if c.MaxPages > 0 {
		return c.MaxPages
	}

	return 1
}

// FetchReleases fetches releases from GitHub. Drafts are filtered out
// unconditionally; pre-releases are retained and the caller decides
// whether to surface them.
//
// On HTTP non-2xx responses the body is included in the wrapped error
// so the caller can map to a meaningful gRPC code. Rate-limit headers
// are inspected and a Warn is logged when the remaining quota dips.
func (c *GitHubReleasesClient) FetchReleases(ctx context.Context) ([]Release, error) {
	if c.Repo == "" {
		return nil, fmt.Errorf("github releases: repo not configured")
	}

	maxPages := c.maxPages()
	all := make([]Release, 0, c.perPage()*maxPages)

	for page := 1; page <= maxPages; page++ {
		batch, err := c.fetchPage(ctx, page)
		if err != nil {
			return nil, err
		}

		if len(batch) == 0 {
			break
		}

		all = append(all, batch...)

		if len(batch) < c.perPage() {
			break
		}
	}

	return all, nil
}

func (c *GitHubReleasesClient) fetchPage(ctx context.Context, page int) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d&page=%d",
		c.baseURL(), c.Repo, c.perPage(), page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github releases: build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("github releases: do request: %w", err)
	}
	defer resp.Body.Close()

	c.checkRateLimit(resp)

	body, err := io.ReadAll(io.LimitReader(resp.Body, githubReleasesBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("github releases: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Truncate body for the error message.
		const errExcerptLen = 256

		excerpt := string(body)
		if len(excerpt) > errExcerptLen {
			excerpt = excerpt[:errExcerptLen]
		}

		return nil, fmt.Errorf("github releases: status %d: %s", resp.StatusCode, excerpt)
	}

	var raw []struct {
		PublishedAt time.Time `json:"published_at"`
		Tag         string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		Assets      []struct {
			Name        string `json:"name"`
			DownloadURL string `json:"browser_download_url"`
			ContentType string `json:"content_type"`
			Size        int64  `json:"size"`
		} `json:"assets"`
		Draft      bool `json:"draft"`
		Prerelease bool `json:"prerelease"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("github releases: parse json: %w", err)
	}

	out := make([]Release, 0, len(raw))

	for _, r := range raw {
		if r.Draft {
			continue
		}

		rel := Release{
			Tag:         r.Tag,
			Name:        r.Name,
			Body:        r.Body,
			PublishedAt: r.PublishedAt,
			Prerelease:  r.Prerelease,
			Assets:      make([]Asset, 0, len(r.Assets)),
		}

		if v, err := ParseTag(r.Tag); err == nil {
			rel.Version = v.Canonical()
		}

		for _, a := range r.Assets {
			rel.Assets = append(rel.Assets, Asset{
				Name:        a.Name,
				DownloadURL: a.DownloadURL,
				ContentType: a.ContentType,
				SizeBytes:   a.Size,
			})
		}

		out = append(out, rel)
	}

	return out, nil
}

// checkRateLimit logs a Warn when the remaining quota drops below 10.
func (c *GitHubReleasesClient) checkRateLimit(resp *http.Response) {
	rem := resp.Header.Get("X-RateLimit-Remaining")
	if rem == "" {
		return
	}

	n, err := strconv.Atoi(rem)
	if err != nil {
		return
	}

	if n < 10 {
		reset := resp.Header.Get("X-RateLimit-Reset")
		c.Log.Warn().
			Int("remaining", n).
			Str("reset_unix", reset).
			Msg("github API rate limit running low")
	}
}
