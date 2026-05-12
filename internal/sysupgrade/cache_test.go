package sysupgrade

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(filepath.Join(dir, "cache.json"))

	releases := []Release{
		{Tag: "v1.8.0", Version: "1.8.0", Assets: []Asset{{Name: "x", DownloadURL: "u"}}},
		{Tag: "v1.7.0", Version: "1.7.0"},
	}

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, c.Save(context.Background(), releases, now))

	// Fresh cache instance to force a disk read.
	c2 := NewDiskCache(filepath.Join(dir, "cache.json"))
	got, fetchedAt, err := c2.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, releases, got)
	assert.True(t, now.Equal(fetchedAt))
}

func TestDiskCache_Load_Missing(t *testing.T) {
	c := NewDiskCache(filepath.Join(t.TempDir(), "missing.json"))

	got, fetchedAt, err := c.Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.True(t, fetchedAt.IsZero())
}
