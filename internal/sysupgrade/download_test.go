package sysupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// fakeStatfs returns the supplied free bytes.
func fakeStatfs(freeBytes int64) func(string, *unix.Statfs_t) error {
	return func(_ string, st *unix.Statfs_t) error {
		st.Bsize = 4096
		st.Bavail = uint64(freeBytes / 4096)

		return nil
	}
}

func TestStreamDownloadVerify_Happy(t *testing.T) {
	payload := []byte("hello sysupgrade payload")
	digest := sha256.Sum256(payload)
	expected := hex.EncodeToString(digest[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "image.bin")

	var mu sync.Mutex

	events := []Progress{}
	sink := func(p Progress) {
		mu.Lock()
		defer mu.Unlock()

		events = append(events, p)
	}

	got, err := streamDownloadVerify(context.Background(), downloadOptions{
		HTTP:         srv.Client(),
		StatfsFunc:   fakeStatfs(1024 * 1024),
		Asset:        Asset{Name: "image.bin", DownloadURL: srv.URL + "/img", SizeBytes: int64(len(payload))},
		DestPath:     dest,
		ExpectedHex:  expected,
		ProgressSink: sink,
	})
	require.NoError(t, err)
	assert.Equal(t, dest, got)

	got2, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, payload, got2)
	assert.NotEmpty(t, events, "expected at least the terminal progress emission")
}

func TestStreamDownloadVerify_ChecksumMismatch(t *testing.T) {
	payload := []byte("hello sysupgrade payload")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "image.bin")

	_, err := streamDownloadVerify(context.Background(), downloadOptions{
		HTTP:        srv.Client(),
		StatfsFunc:  fakeStatfs(1024 * 1024),
		Asset:       Asset{Name: "image.bin", DownloadURL: srv.URL + "/img", SizeBytes: int64(len(payload))},
		DestPath:    dest,
		ExpectedHex: "deadbeef",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrChecksumMismatch))

	_, statErr := os.Stat(dest)
	assert.True(t, os.IsNotExist(statErr), "partial file must be removed on mismatch")
}

func TestStreamDownloadVerify_InsufficientSpace(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "image.bin")

	_, err := streamDownloadVerify(context.Background(), downloadOptions{
		HTTP:       http.DefaultClient,
		StatfsFunc: fakeStatfs(1024), // only 1 KiB free
		Asset:      Asset{Name: "image.bin", DownloadURL: "http://unused", SizeBytes: 100 * 1024},
		DestPath:   dest,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInsufficientSpace))
}

func TestStreamDownloadVerify_HTTPNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "image.bin")

	_, err := streamDownloadVerify(context.Background(), downloadOptions{
		HTTP:       srv.Client(),
		StatfsFunc: fakeStatfs(1024 * 1024),
		Asset:      Asset{Name: "image.bin", DownloadURL: srv.URL, SizeBytes: 0},
		DestPath:   dest,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}
