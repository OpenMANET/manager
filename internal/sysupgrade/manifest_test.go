package sysupgrade

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleManifestJSON = `{
  "boards": {
    "bcm2711,mm8108-usb": "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz"
  }
}`

func TestFetchManifest_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleManifestJSON))
	}))
	t.Cleanup(srv.Close)

	rel := Release{
		Assets: []Asset{
			{Name: "manifest.json", DownloadURL: srv.URL + "/manifest.json"},
			{Name: "image.img.gz", DownloadURL: "u"},
		},
	}

	m, err := FetchManifest(context.Background(), srv.Client(), rel)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz", m.Boards["bcm2711,mm8108-usb"])
}

func TestFetchManifest_NotFound(t *testing.T) {
	rel := Release{
		Assets: []Asset{
			{Name: "image.img.gz", DownloadURL: "u"},
		},
	}

	_, err := FetchManifest(context.Background(), http.DefaultClient, rel)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrManifestNotFound))
}
