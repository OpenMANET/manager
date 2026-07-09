package sysupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/system"
	"github.com/openmanet/openmanetd/internal/util/board"
)

// fakeBoardProvider returns a stub board.
type fakeBoardProvider struct {
	board *board.Board
	err   error
}

func (f *fakeBoardProvider) GetBoard() (*board.Board, error) {
	return f.board, f.err
}

// fakeFirmwareProvider returns a stub FirmwareInfo.
type fakeFirmwareProvider struct {
	info *system.FirmwareInfo
	err  error
}

func (f *fakeFirmwareProvider) GetFirmwareInfo() (*system.FirmwareInfo, error) {
	return f.info, f.err
}

// fakeSysInfoProvider returns canned strings.
type fakeSysInfoProvider struct {
	hostname string
	kernel   string
	arch     string
	err      error
}

func (f *fakeSysInfoProvider) GetHostname() (string, error) {
	return f.hostname, f.err
}

func (f *fakeSysInfoProvider) GetKernelVersion() (string, error) {
	return f.kernel, f.err
}

func (f *fakeSysInfoProvider) GetArchitecture() (string, error) {
	return f.arch, f.err
}

// fakeCapabilityProvider returns a stub capability.
type fakeCapabilityProvider struct {
	cap *system.SysupgradeCapability
	err error
}

func (f *fakeCapabilityProvider) GetSysupgradeCapability() (*system.SysupgradeCapability, error) {
	return f.cap, f.err
}

// fakeReleasesFetcher returns a canned release list. Increments call
// counter so tests can verify cache behavior.
type fakeReleasesFetcher struct {
	mu       sync.Mutex
	releases []Release
	calls    int
	err      error
}

func (f *fakeReleasesFetcher) FetchReleases(_ context.Context) ([]Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	return f.releases, f.err
}

func (f *fakeReleasesFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// fakeManifestFetcher always returns ErrManifestNotFound (so heuristic
// matching is exercised in the tests).
type fakeManifestFetcher struct{}

func (fakeManifestFetcher) FetchManifest(_ context.Context, _ Release) (*Manifest, error) {
	return nil, ErrManifestNotFound
}

// fakeRunner records every Run call.
type fakeRunner struct {
	mu             sync.Mutex
	pid            int
	err            error
	preflightErr   error
	calls          int
	preflightCalls int
	lastImage      string
	lastLog        string
	lastPreflight  string
	lastOpts       SysupgradeOptions
}

func (f *fakeRunner) Run(_ context.Context, image, logPath string, opts SysupgradeOptions) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	f.lastImage = image
	f.lastLog = logPath
	f.lastOpts = opts

	if f.err != nil {
		return 0, f.err
	}

	pid := f.pid
	if pid == 0 {
		pid = 4242
	}

	return pid, nil
}

func (f *fakeRunner) Preflight(_ context.Context, image string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.preflightCalls++
	f.lastPreflight = image

	return f.preflightErr
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

func (f *fakeRunner) preflightCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.preflightCalls
}

// inMemoryCache satisfies ReleaseCache without touching disk.
type inMemoryCache struct {
	mu        sync.Mutex
	releases  []Release
	fetchedAt time.Time
}

func (c *inMemoryCache) Load(_ context.Context) ([]Release, time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]Release, len(c.releases))
	copy(out, c.releases)

	return out, c.fetchedAt, nil
}

func (c *inMemoryCache) Save(_ context.Context, r []Release, at time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.releases = append([]Release(nil), r...)
	c.fetchedAt = at

	return nil
}

// makeManager wires a manager with the supplied dependencies and
// reasonable defaults for everything else.
func makeManager(t *testing.T, fetcher ReleasesFetcher, runner SysupgradeRunner, currentVer string) *Manager {
	t.Helper()

	desc := ""
	if currentVer != "" {
		desc = "OpenMANET " + currentVer
	}

	mgr := NewManager(Options{
		Log:  zerolog.Nop(),
		Repo: "OpenMANET/firmware",
		HTTP: http.DefaultClient,
		Board: &fakeBoardProvider{board: &board.Board{
			Model:  board.Model{ID: "bcm2711,mm8108-usb", Name: "RPi"},
			System: board.System{Hostname: "host"},
		}},
		Firmware: &fakeFirmwareProvider{info: &system.FirmwareInfo{
			Description:  desc,
			Distribution: "OpenMANET",
			Target:       "bcm27xx/bcm2711",
			OpenMANETVer: currentVer,
		}},
		SysInfo: &fakeSysInfoProvider{hostname: "host", kernel: "6.6.0", arch: "aarch64"},
		Capable: &fakeCapabilityProvider{cap: &system.SysupgradeCapability{
			Capable:        true,
			Reason:         "ok",
			RootFSType:     "squashfs",
			BinaryPresent:  true,
			OverlayPresent: true,
			BinaryPath:     "/sbin/sysupgrade",
		}},
		Cache:            &inMemoryCache{},
		Releases:         fetcher,
		Manifest:         fakeManifestFetcher{},
		Runner:           runner,
		DownloadDir:      t.TempDir(),
		PersistentLogDir: t.TempDir(),
	})

	// Drain in-flight upgrade / watcher goroutines before the test's
	// t.TempDir cleanup runs RemoveAll on the download / persistent log
	// directories. t.Cleanup is LIFO, so a Cleanup registered here runs
	// before the TempDir cleanups registered above. Without this, the
	// per-upgrade goroutine spawned by StartLocalUpgrade can race with
	// RemoveAll and leave the parent temp directory non-empty.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := mgr.Shutdown(ctx); err != nil {
			t.Errorf("sysupgrade manager shutdown: %v", err)
		}
	})

	return mgr
}

// makeAsset is a small helper for building test assets.
func makeAsset(name, url string, size int64) Asset {
	return Asset{Name: name, DownloadURL: url, SizeBytes: size}
}

func TestManager_GetSystemInfo(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	info, err := mgr.GetSystemInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "host", info.Hostname)
	assert.Equal(t, "bcm2711,mm8108-usb", info.BoardName)
	assert.Equal(t, "RPi", info.Model)
	assert.Equal(t, "bcm27xx/bcm2711", info.Target)
	assert.Equal(t, "1.7.0", info.OpenmanetVersion)
	assert.Equal(t, "6.6.0", info.Kernel)
	assert.Equal(t, "aarch64", info.Architecture)
	assert.True(t, info.SysupgradeCapable)
	assert.Equal(t, "ok", info.SysupgradeCapableReason)
}

func TestManager_ListAvailableUpdates_FilterAndSort(t *testing.T) {
	releases := []Release{
		{Tag: "v1.6.0", Assets: []Asset{makeAsset("openmanet-1.6.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz", "u-old", 1)}},
		{Tag: "v1.8.0", Assets: []Asset{makeAsset("openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz", "u-new", 1)}},
		{Tag: "v1.9.0-rc.1", Prerelease: true, Assets: []Asset{makeAsset("openmanet-1.9.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz", "u-rc", 1)}},
	}
	fetcher := &fakeReleasesFetcher{releases: releases}

	mgr := makeManager(t, fetcher, &fakeRunner{}, "1.7.0")

	// Without prereleases.
	updates, _, err := mgr.ListAvailableUpdates(context.Background(), true, false)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.Equal(t, "v1.8.0", updates[0].Release.Tag)

	// With prereleases.
	updatesPre, _, err := mgr.ListAvailableUpdates(context.Background(), false, true)
	require.NoError(t, err)
	require.Len(t, updatesPre, 2)
	assert.Equal(t, "v1.9.0-rc.1", updatesPre[0].Release.Tag, "expected newest first")
	assert.Equal(t, "v1.8.0", updatesPre[1].Release.Tag)

	// Cache should mean the second call did not increase the call count.
	assert.Equal(t, 1, fetcher.callCount(), "cache should serve second request")
}

func TestManager_ListAvailableUpdates_UnknownCurrentVersion(t *testing.T) {
	releases := []Release{
		{Tag: "v1.5.0", Assets: []Asset{makeAsset("openmanet-1.5.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz", "u", 1)}},
	}
	fetcher := &fakeReleasesFetcher{releases: releases}

	// Empty current version → return the release with NewerThanCurrent=false.
	mgr := makeManager(t, fetcher, &fakeRunner{}, "")

	updates, _, err := mgr.ListAvailableUpdates(context.Background(), true, false)
	require.NoError(t, err)
	require.Len(t, updates, 1)
	assert.False(t, updates[0].NewerThanCurrent)
}

func TestManager_GetReleaseDetail(t *testing.T) {
	releases := []Release{
		{Tag: "v1.8.0", Body: "release body"},
	}
	fetcher := &fakeReleasesFetcher{releases: releases}
	mgr := makeManager(t, fetcher, &fakeRunner{}, "1.7.0")

	rel, err := mgr.GetReleaseDetail(context.Background(), "v1.8.0")
	require.NoError(t, err)
	assert.Equal(t, "release body", rel.Body)

	_, err = mgr.GetReleaseDetail(context.Background(), "v9.9.9")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrReleaseNotFound))
}

func TestManager_StartUpgrade_UnknownCurrentVersion(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "")

	err := mgr.StartUpgrade(context.Background(), "v1.8.0", "x", SysupgradeOptions{}, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownCurrentVersion))
}

func TestManager_StartUpgrade_OptionConflict(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	err := mgr.StartUpgrade(context.Background(), "v1.8.0", "x", SysupgradeOptions{NoPreserveConfig: true, PreserveChangedEtc: true}, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOptionConflict))
}

func TestManager_StartUpgrade_HappyPath(t *testing.T) {
	imgPayload := []byte("openmanet image bytes")
	digest := sha256.Sum256(imgPayload)
	expectedHex := hex.EncodeToString(digest[:])

	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(imgPayload)
	}))
	t.Cleanup(imgServer.Close)

	sumsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz\n", expectedHex)
	}))
	t.Cleanup(sumsServer.Close)

	releases := []Release{
		{
			Tag: "v1.8.0",
			Assets: []Asset{
				makeAsset("openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz",
					imgServer.URL+"/img", int64(len(imgPayload))),
				makeAsset("sha256sums", sumsServer.URL+"/sha256sums", 200),
			},
		},
	}
	fetcher := &fakeReleasesFetcher{releases: releases}
	runner := &fakeRunner{pid: 9999}

	mgr := makeManager(t, fetcher, runner, "1.7.0")

	// Pre-populate cache.
	_, _, err := mgr.ListAvailableUpdates(context.Background(), true, false)
	require.NoError(t, err)

	// Subscribe before starting so we can observe events.
	ch, unsub := mgr.Subscribe(context.Background())
	defer unsub()

	err = mgr.StartUpgrade(context.Background(), "v1.8.0",
		"openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz",
		SysupgradeOptions{TestOnly: true}, false)
	require.NoError(t, err)

	// Drain events until we see PhaseUpgrading or time out.
	deadline := time.Now().Add(5 * time.Second)
	sawUpgrading := false

	for time.Now().Before(deadline) && !sawUpgrading {
		select {
		case ev, ok := <-ch:
			if !ok {
				break
			}

			if ev.Phase == PhaseUpgrading {
				sawUpgrading = true

				assert.Equal(t, int32(9999), ev.ChildPID)
			}
		case <-time.After(100 * time.Millisecond):
		}
	}

	require.True(t, sawUpgrading, "expected to observe PhaseUpgrading")

	// Runner must have been called exactly once with the verified image.
	assert.Equal(t, 1, runner.callCount())
	assert.Contains(t, runner.lastImage, "openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz")
	assert.True(t, runner.lastOpts.TestOnly)
	assert.Equal(t, filepath.Dir(runner.lastImage)+"/openmanet-1.8.0-bcm27xx-bcm2711-mm8108-usb-squashfs-sysupgrade.img.gz.log", runner.lastLog)
}

func TestManager_Shutdown_NoUpgradeIsNoop(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	// With nothing running, Shutdown should return immediately even
	// when called multiple times.
	require.NoError(t, mgr.Shutdown(context.Background()))
	require.NoError(t, mgr.Shutdown(context.Background()))
}

func TestManager_Shutdown_WaitsForLocalUpgradeGoroutines(t *testing.T) {
	// blockingRunner makes Run hang until Shutdown's ctx cancel reaches
	// it via the upgrade context. This exercises the cancel-and-wait
	// path: without Shutdown propagating cancel into the goroutine, the
	// test would deadlock on Shutdown's wg.Wait().
	runner := &blockingRunner{started: make(chan struct{}), released: make(chan struct{})}
	mgr := makeManager(t, &fakeReleasesFetcher{}, runner, "1.7.0")

	_, err := mgr.StoreStagedImage(context.Background(), strings.NewReader("payload"), "x.img")
	require.NoError(t, err)
	require.NoError(t, mgr.StartLocalUpgrade(context.Background(), SysupgradeOptions{}, false, false))

	// Wait until the runner has been entered so we know the goroutine
	// is actually parked inside Run.
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner.Run never invoked")
	}

	require.NoError(t, mgr.Shutdown(context.Background()))
	// runner.Run must have been released by the propagated ctx cancel.
	select {
	case <-runner.released:
	default:
		t.Fatal("runner did not observe ctx cancellation")
	}
}

func TestManager_Shutdown_CancelsWatcherAfterChildLaunched(t *testing.T) {
	// Regression: once the runner launches the detached child and the
	// manager reaches PhaseUpgrading, the watcher must remain cancelable
	// by Shutdown. A live child PID (the test process itself) means the
	// watcher never self-exits, so Shutdown returning is the only proof
	// its context cancellation reaches the watcher. Before the fix the
	// watcher ran in its own goroutine that outlived upgradeCancel, and
	// Shutdown blocked on wg.Wait until the watcher's 30-minute deadline.
	runner := &fakeRunner{pid: os.Getpid()}
	mgr := makeManager(t, &fakeReleasesFetcher{}, runner, "1.7.0")

	ch, unsub := mgr.Subscribe(context.Background())
	defer unsub()

	_, err := mgr.StoreStagedImage(context.Background(), strings.NewReader("payload"), "x.img")
	require.NoError(t, err)
	require.NoError(t, mgr.StartLocalUpgrade(context.Background(), SysupgradeOptions{}, false, false))

	// Wait until the watcher is actually running (PhaseUpgrading is
	// published immediately before the manager enters the watch loop).
	deadline := time.Now().Add(5 * time.Second)
	sawUpgrading := false

	for time.Now().Before(deadline) && !sawUpgrading {
		select {
		case ev, ok := <-ch:
			if !ok {
				break
			}

			if ev.Phase == PhaseUpgrading {
				sawUpgrading = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}

	require.True(t, sawUpgrading, "expected to observe PhaseUpgrading")

	// Shutdown must cancel the watcher and return well within the
	// watcher's poll cadence / 30-minute deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, mgr.Shutdown(ctx))
}

// blockingRunner satisfies SysupgradeRunner, parking Run until ctx is
// done so a test can observe Shutdown-driven cancellation.
type blockingRunner struct {
	started  chan struct{}
	released chan struct{}
}

func (b *blockingRunner) Run(ctx context.Context, _, _ string, _ SysupgradeOptions) (int, error) {
	close(b.started)
	<-ctx.Done()
	close(b.released)

	return 0, fmt.Errorf("blockingRunner: %w", ctx.Err())
}

func (b *blockingRunner) Preflight(_ context.Context, _ string) error { return nil }

func TestManager_CancelUpgrade_NoneRunning(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	err := mgr.CancelUpgrade(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoUpgradeInProgress))
}

func TestManager_Subscribe_DropsSlowSubscribers(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	ch, unsub := mgr.Subscribe(context.Background())
	defer unsub()

	// Fill the buffer + one extra.
	for i := 0; i <= subscriberQueue+5; i++ {
		mgr.setProgress(Progress{Phase: PhaseChecking, Message: "tick"})
	}

	// At least subscriberQueue events should be readable; further
	// events should have been dropped without blocking.
	read := 0

	for {
		select {
		case <-ch:
			read++
		case <-time.After(20 * time.Millisecond):
			assert.LessOrEqual(t, read, subscriberQueue,
				"slow subscriber should have dropped overflow events")

			return
		}
	}
}
