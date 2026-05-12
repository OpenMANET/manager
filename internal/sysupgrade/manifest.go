package sysupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// manifestAssetName is the conventional filename for a release manifest.
const manifestAssetName = "manifest.json"

// manifestBodyLimit caps the manifest size read into memory.
const manifestBodyLimit = 256 << 10 // 256 KiB

// ErrManifestNotFound is returned by FetchManifest when the release does
// not include a manifest.json asset.
var ErrManifestNotFound = errors.New("sysupgrade: manifest.json not found in release")

// FetchManifest downloads and parses the manifest.json asset from the
// given release, if one is present. Returns ErrManifestNotFound when no
// manifest.json asset is attached. All other errors propagate as-is.
//
// httpClient may be nil; the helper falls back to http.DefaultClient.
func FetchManifest(ctx context.Context, httpClient *http.Client, rel Release) (*Manifest, error) {
	for _, a := range rel.Assets {
		if a.Name != manifestAssetName {
			continue
		}

		return downloadManifest(ctx, httpClient, a)
	}

	return nil, ErrManifestNotFound
}

// downloadManifest performs the HTTP GET against the manifest asset and
// parses it. Helper kept separate so tests can drive it without
// constructing a full Release.
func downloadManifest(ctx context.Context, httpClient *http.Client, a Asset) (*Manifest, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.DownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("manifest: build request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, manifestBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("manifest: read body: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse json: %w", err)
	}

	return &m, nil
}
