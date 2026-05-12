package sysupgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_StoreStagedImage_HappyPath(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	payload := []byte("uploaded firmware bytes")
	digest := sha256.Sum256(payload)
	expectedHex := hex.EncodeToString(digest[:])

	staged, err := mgr.StoreStagedImage(
		context.Background(),
		bytes.NewReader(payload),
		"openmanet-1.9.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz",
	)
	require.NoError(t, err)
	require.NotNil(t, staged)

	assert.Equal(t, "openmanet-1.9.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz", staged.Filename)
	assert.Equal(t, int64(len(payload)), staged.SizeBytes)
	assert.Equal(t, expectedHex, staged.Sha256)
	// Plain ASCII payload has no FWx0 trailer; compat fields stay zeroed.
	assert.False(t, staged.MetadataPresent, "plain payload has no FWx0 metadata")
	assert.False(t, staged.ImageCompatible, "no metadata means no compat verdict")
	assert.True(t, staged.PreflightOK)
	assert.Empty(t, staged.PreflightError)
	assert.False(t, staged.UploadedAt.IsZero())

	// File on disk should match the payload exactly.
	got, readErr := os.ReadFile(staged.Path)
	require.NoError(t, readErr)
	assert.Equal(t, payload, got)

	// GetStagedImage returns a defensive copy.
	mirror := mgr.GetStagedImage()
	require.NotNil(t, mirror)
	assert.Equal(t, staged.Sha256, mirror.Sha256)

	// Mutating the copy doesn't reach manager state.
	mirror.Sha256 = "tampered"
	stillOK := mgr.GetStagedImage()
	assert.Equal(t, expectedHex, stillOK.Sha256)
}

func TestManager_StoreStagedImage_PreflightFailure(t *testing.T) {
	runner := &fakeRunner{preflightErr: errors.New("Image check failed: bad magic")}
	mgr := makeManager(t, &fakeReleasesFetcher{}, runner, "1.7.0")

	staged, err := mgr.StoreStagedImage(
		context.Background(),
		bytes.NewReader([]byte("x")),
		"random-image.bin",
	)
	require.NoError(t, err)
	require.NotNil(t, staged)
	assert.False(t, staged.PreflightOK)
	assert.Contains(t, staged.PreflightError, "bad magic")
	assert.Equal(t, 1, runner.preflightCallCount())
}

func TestManager_StoreStagedImage_RejectsEmptyAndOversize(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	_, err := mgr.StoreStagedImage(context.Background(), bytes.NewReader(nil), "x.img")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUploadEmpty))

	// Build an oversize reader without allocating the full 512MB.
	oversize := io.MultiReader(
		// Repeats a small chunk many times to exceed MaxStagedImageBytes.
		strings.NewReader(strings.Repeat("A", 1024)),
		&infiniteByteReader{b: 'A'},
	)
	_, err = mgr.StoreStagedImage(context.Background(), oversize, "x.img")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUploadTooLarge))
}

// infiniteByteReader produces an unbounded stream of a single byte.
type infiniteByteReader struct{ b byte }

func (r *infiniteByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}

	return len(p), nil
}

func TestManager_StoreStagedImage_ReplaceExisting(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	first, err := mgr.StoreStagedImage(context.Background(), strings.NewReader("aaa"), "first.img")
	require.NoError(t, err)

	firstPath := first.Path

	second, err := mgr.StoreStagedImage(context.Background(), strings.NewReader("bbbb"), "second.img")
	require.NoError(t, err)

	assert.Equal(t, firstPath, second.Path, "canonical path is reused")
	got, _ := os.ReadFile(second.Path)
	assert.Equal(t, []byte("bbbb"), got)

	mirror := mgr.GetStagedImage()
	assert.Equal(t, "second.img", mirror.Filename)
}

func TestManager_DiscardStagedImage(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	// Discard with nothing staged returns the sentinel.
	err := mgr.DiscardStagedImage()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoStagedImage))

	staged, err := mgr.StoreStagedImage(context.Background(), strings.NewReader("xxx"), "x.img")
	require.NoError(t, err)
	require.FileExists(t, staged.Path)

	require.NoError(t, mgr.DiscardStagedImage())
	assert.NoFileExists(t, staged.Path)
	assert.Nil(t, mgr.GetStagedImage())
}

func TestManager_StartLocalUpgrade_HappyPath(t *testing.T) {
	runner := &fakeRunner{pid: 31337}
	mgr := makeManager(t, &fakeReleasesFetcher{}, runner, "1.7.0")

	staged, err := mgr.StoreStagedImage(context.Background(), strings.NewReader("payload"), "openmanet-bcm27xx-bcm2711.img")
	require.NoError(t, err)
	require.True(t, staged.PreflightOK)

	ch, unsub := mgr.Subscribe(context.Background())
	defer unsub()

	require.NoError(t, mgr.StartLocalUpgrade(context.Background(), SysupgradeOptions{}, false, false))

	// Drain progress events looking for PhaseUpgrading.
	sawUpgrading := false
	for i := 0; i < 20 && !sawUpgrading; i++ {
		ev, ok := <-ch
		if !ok {
			break
		}

		if ev.Phase == PhaseUpgrading {
			sawUpgrading = true

			assert.Equal(t, int32(31337), ev.ChildPID)
			assert.Equal(t, staged.Filename, ev.AssetName)
		}
	}

	require.True(t, sawUpgrading, "expected to observe PhaseUpgrading")
	assert.Equal(t, 1, runner.callCount())
	// The image path the runner was called against must match the
	// staged file path on disk.
	assert.Equal(t, staged.Path, runner.lastImage)
	assert.Equal(t, filepath.Join(filepath.Dir(staged.Path), stagedFilename+".log"), runner.lastLog)
}

func TestManager_StartLocalUpgrade_BlockedByFailedPreflight(t *testing.T) {
	runner := &fakeRunner{preflightErr: errors.New("incompatible")}
	mgr := makeManager(t, &fakeReleasesFetcher{}, runner, "1.7.0")

	_, err := mgr.StoreStagedImage(context.Background(), strings.NewReader("p"), "wrong.img")
	require.NoError(t, err)

	err = mgr.StartLocalUpgrade(context.Background(), SysupgradeOptions{}, false, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStagedPreflightFailed))

	// skipPreflight=true overrides the gate.
	require.NoError(t, mgr.StartLocalUpgrade(context.Background(), SysupgradeOptions{}, true, false))
}

func TestManager_StartLocalUpgrade_NoImageStaged(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	err := mgr.StartLocalUpgrade(context.Background(), SysupgradeOptions{}, false, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoStagedImage))
}

func TestManager_StartLocalUpgrade_UnknownVersion(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "")

	_, err := mgr.StoreStagedImage(context.Background(), strings.NewReader("p"), "x.img")
	require.NoError(t, err)

	err = mgr.StartLocalUpgrade(context.Background(), SysupgradeOptions{}, false, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownCurrentVersion))

	// forceUnknown=true overrides the gate.
	require.NoError(t, mgr.StartLocalUpgrade(context.Background(), SysupgradeOptions{}, false, true))
}

func TestCleanUploadedFilename(t *testing.T) {
	cases := map[string]string{
		"":                "uploaded-image.bin",
		"   ":             "uploaded-image.bin",
		"plain.img":       "plain.img",
		"/etc/passwd":     "passwd",
		`C:\evil\bad.img`: "bad.img",
		"trailing/":       "uploaded-image.bin",
	}
	for in, want := range cases {
		assert.Equal(t, want, cleanUploadedFilename(in), "input=%q", in)
	}
}
