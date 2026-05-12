package sysupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ReleaseCache stores the most recently fetched release list. The
// in-memory cache is always populated; the on-disk JSON file is best-effort
// — read errors fall back to a fetch and write errors are logged but
// non-fatal.
type ReleaseCache interface {
	Load(ctx context.Context) ([]Release, time.Time, error)
	Save(ctx context.Context, releases []Release, fetchedAt time.Time) error
}

// DiskCache is a JSON-file backed implementation of ReleaseCache. It is
// safe for concurrent use.
type DiskCache struct {
	fetchedAt time.Time
	Path      string
	mem       []Release
	mu        sync.Mutex
	loaded    bool
}

// NewDiskCache constructs a DiskCache backed by the supplied path.
func NewDiskCache(path string) *DiskCache {
	return &DiskCache{Path: path}
}

// cacheFile is the on-disk JSON shape.
type cacheFile struct {
	FetchedAt time.Time `json:"fetched_at"`
	Releases  []Release `json:"releases"`
}

// Load returns the cached releases and the timestamp they were fetched
// at. Returns an empty slice and zero time if the file does not exist.
func (c *DiskCache) Load(_ context.Context) ([]Release, time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loaded {
		return c.mem, c.fetchedAt, nil
	}

	data, err := os.ReadFile(c.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			c.loaded = true

			return nil, time.Time{}, nil
		}

		return nil, time.Time{}, fmt.Errorf("sysupgrade cache: read: %w", err)
	}

	var f cacheFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, time.Time{}, fmt.Errorf("sysupgrade cache: parse: %w", err)
	}

	c.mem = f.Releases
	c.fetchedAt = f.FetchedAt
	c.loaded = true

	return c.mem, c.fetchedAt, nil
}

// Save atomically writes the supplied releases to disk via temp file
// rename. The in-memory cache is updated regardless of disk-write
// success so a transient ENOSPC does not poison the in-process state.
func (c *DiskCache) Save(_ context.Context, releases []Release, fetchedAt time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.mem = releases
	c.fetchedAt = fetchedAt
	c.loaded = true

	if c.Path == "" {
		return nil
	}

	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sysupgrade cache: mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "sysupgrade-releases-*.tmp")
	if err != nil {
		return fmt.Errorf("sysupgrade cache: temp: %w", err)
	}

	tmpName := tmp.Name()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")

	if err := enc.Encode(cacheFile{FetchedAt: fetchedAt, Releases: releases}); err != nil {
		tmp.Close()
		os.Remove(tmpName)

		return fmt.Errorf("sysupgrade cache: encode: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)

		return fmt.Errorf("sysupgrade cache: fsync: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)

		return fmt.Errorf("sysupgrade cache: close: %w", err)
	}

	if err := os.Rename(tmpName, c.Path); err != nil {
		os.Remove(tmpName)

		return fmt.Errorf("sysupgrade cache: rename: %w", err)
	}

	return nil
}
