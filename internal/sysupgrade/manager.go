package sysupgrade

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/system"
	"github.com/openmanet/openmanetd/internal/util/board"
)

// Sentinel errors returned by Manager methods. Handlers map these to
// gRPC codes.
var (
	// ErrUnknownCurrentVersion is returned when the running OpenMANET
	// version cannot be parsed and the caller did not request
	// force_install_unknown_current.
	ErrUnknownCurrentVersion = errors.New("sysupgrade: current version unknown; pass force_install_unknown_current to override")

	// ErrBusy is returned when StartUpgrade is called while a previous
	// upgrade is still in flight.
	ErrBusy = errors.New("sysupgrade: an upgrade is already in progress")

	// ErrNoUpgradeInProgress is returned by CancelUpgrade when no
	// upgrade is running.
	ErrNoUpgradeInProgress = errors.New("sysupgrade: no upgrade in progress")

	// ErrUpgradeAlreadyExecuting is returned by CancelUpgrade when the
	// sysupgrade binary has already been launched.
	ErrUpgradeAlreadyExecuting = errors.New("sysupgrade: upgrade already executing")

	// ErrReleaseNotFound is returned when GetReleaseDetail or
	// StartUpgrade reference a tag that does not exist in the cache.
	ErrReleaseNotFound = errors.New("sysupgrade: release not found")
)

// BoardProvider returns the parsed /etc/board.json contents. The handler
// already wraps a CachedBoardProvider; the manager accepts the interface
// directly to avoid a transitive dependency.
type BoardProvider interface {
	GetBoard() (*board.Board, error)
}

// FirmwareProvider returns the parsed /etc/openwrt_release contents.
type FirmwareProvider interface {
	GetFirmwareInfo() (*system.FirmwareInfo, error)
}

// SysInfoProvider supplies the kernel + arch + hostname strings.
type SysInfoProvider interface {
	GetHostname() (string, error)
	GetKernelVersion() (string, error)
	GetArchitecture() (string, error)
}

// CapabilityProvider returns sysupgrade capability info.
type CapabilityProvider interface {
	GetSysupgradeCapability() (*system.SysupgradeCapability, error)
}

// ReleasesFetcher loads the GitHub releases for the configured repo.
type ReleasesFetcher interface {
	FetchReleases(ctx context.Context) ([]Release, error)
}

// ManifestFetcher fetches the optional manifest.json asset of a release.
// Implementations should return ErrManifestNotFound when the asset is
// absent.
type ManifestFetcher interface {
	FetchManifest(ctx context.Context, rel Release) (*Manifest, error)
}

// HTTPClient is the subset of *http.Client used by the manager. Kept as
// an interface so tests can substitute a transport-only fake.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Options bundles the manager construction inputs.
type Options struct {
	Log                 zerolog.Logger
	Repo                string // "OpenMANET/firmware"
	HTTP                *http.Client
	Board               BoardProvider
	Firmware            FirmwareProvider
	SysInfo             SysInfoProvider
	Capable             CapabilityProvider
	Cache               ReleaseCache
	Releases            ReleasesFetcher
	Manifest            ManifestFetcher
	Runner              SysupgradeRunner
	FactoryReset        FactoryResetRunner
	FactoryResetCapable system.FactoryResetCapabilityProvider

	// DownloadDir is where the staged image and the runtime sysupgrade
	// log file live. tmpfs on OpenWrt — large enough to hold a 50 MB
	// firmware image, but contents are wiped on every reboot.
	DownloadDir string

	// PersistentLogDir is where post-failure breadcrumbs go so the
	// operator can still inspect them after the device reboots. /etc
	// on OpenWrt is the overlay (jffs2 / squashfs+overlay) and survives
	// reboots; the daemon writes a single small last-failure.log here
	// when watchSysupgradeChild detects an early exit.
	PersistentLogDir string
}

// Manager is the central orchestrator for sysupgrade workflows.
type Manager struct {
	log                zerolog.Logger
	releases           ReleasesFetcher
	runner             SysupgradeRunner
	board              BoardProvider
	firmware           FirmwareProvider
	sysInfo            SysInfoProvider
	capable            CapabilityProvider
	cache              ReleaseCache
	manifest           ManifestFetcher
	factoryResetRunner FactoryResetRunner
	factoryResetCap    system.FactoryResetCapabilityProvider
	upgradeCancel      context.CancelFunc
	http               *http.Client
	subs               map[uint64]chan Progress
	staged             *StagedImage
	downloadDir        string
	persistentLogDir   string
	repo               string
	progress           Progress
	nextSub            uint64
	mu                 sync.Mutex
	subsMu             sync.Mutex
	wg                 sync.WaitGroup // tracks runUpgrade / runLocalUpgrade / watchSysupgradeChild goroutines
	upgradeStarted     bool
	uploadInFlight     bool
}

// subscriberQueue is the capacity used for each Subscribe channel.
// Sized for ~800ms of UI-side stall at 100ms emit cadence; full
// channels drop the event for that subscriber rather than blocking.
const subscriberQueue = 8

// NewManager constructs a Manager from Options. Any nil dependency that
// has a sensible default is filled in (HTTP, Cache, Manifest fetcher).
// Required deps without defaults (Board, Firmware, SysInfo, Capable,
// Releases, Runner) panic when missing — this is daemon-startup code,
// not a runtime path.
func NewManager(opts Options) *Manager {
	if opts.Board == nil || opts.Firmware == nil || opts.SysInfo == nil {
		panic("sysupgrade: NewManager: Board / Firmware / SysInfo are required")
	}

	if opts.Capable == nil || opts.Releases == nil || opts.Runner == nil {
		panic("sysupgrade: NewManager: Capable / Releases / Runner are required")
	}

	httpClient := opts.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Minute}
	}

	cache := opts.Cache
	if cache == nil {
		cache = NewDiskCache("")
	}

	manifest := opts.Manifest
	if manifest == nil {
		manifest = &defaultManifestFetcher{HTTP: httpClient}
	}

	dlDir := opts.DownloadDir
	if dlDir == "" {
		dlDir = "/tmp/openmanetd/sysupgrade"
	}

	persistentDir := opts.PersistentLogDir
	if persistentDir == "" {
		persistentDir = "/etc/openmanetd/sysupgrade"
	}

	return &Manager{
		log:                opts.Log,
		repo:               opts.Repo,
		http:               httpClient,
		board:              opts.Board,
		firmware:           opts.Firmware,
		sysInfo:            opts.SysInfo,
		capable:            opts.Capable,
		cache:              cache,
		releases:           opts.Releases,
		manifest:           manifest,
		runner:             opts.Runner,
		factoryResetRunner: opts.FactoryReset,
		factoryResetCap:    opts.FactoryResetCapable,
		downloadDir:        dlDir,
		persistentLogDir:   persistentDir,
		progress:           Progress{Phase: PhaseIdle, UpdatedAt: time.Now()},
		subs:               make(map[uint64]chan Progress, 4),
	}
}

// defaultManifestFetcher is the production ManifestFetcher. It calls
// FetchManifest (the package-level helper) under the hood.
type defaultManifestFetcher struct {
	HTTP *http.Client
}

func (f *defaultManifestFetcher) FetchManifest(ctx context.Context, rel Release) (*Manifest, error) {
	return FetchManifest(ctx, f.HTTP, rel)
}

// GetSystemInfo composes board + firmware + capability + sysinfo into
// the rich snapshot used by the UI's firmware page.
func (m *Manager) GetSystemInfo(_ context.Context) (*SystemInfo, error) {
	out := &SystemInfo{}

	if b, err := m.board.GetBoard(); err == nil && b != nil {
		out.BoardName = b.Model.ID
		out.Model = b.Model.Name

		if out.Hostname == "" {
			out.Hostname = b.System.Hostname
		}
	}

	if fw, err := m.firmware.GetFirmwareInfo(); err == nil && fw != nil {
		out.Distribution = fw.Distribution
		out.Release = fw.Release
		out.Revision = fw.Revision
		out.Target = fw.Target
		out.Description = fw.Description
		out.OpenmanetVersion = fw.OpenMANETVer
		out.BuildDate = fw.BuildDate
	}

	if name, err := m.sysInfo.GetHostname(); err == nil && name != "" {
		out.Hostname = name
	}

	if k, err := m.sysInfo.GetKernelVersion(); err == nil {
		out.Kernel = k
	}

	if a, err := m.sysInfo.GetArchitecture(); err == nil {
		out.Architecture = a
	}

	if cap, err := m.capable.GetSysupgradeCapability(); err == nil && cap != nil {
		out.SysupgradeCapable = cap.Capable
		out.SysupgradeCapableReason = cap.Reason
		out.RootfsType = cap.RootFSType
	}

	return out, nil
}

// ListAvailableUpdates loads the cached releases (or refreshes from
// GitHub when forceRefresh is true) and filters them down to those with
// a matching asset for the local hardware that are also newer than the
// running OpenMANET version. Pre-releases are dropped unless
// includePrerelease is set.
func (m *Manager) ListAvailableUpdates(ctx context.Context, forceRefresh, includePrerelease bool) ([]Update, time.Time, error) {
	releases, fetchedAt, err := m.loadReleases(ctx, forceRefresh)
	if err != nil {
		return nil, time.Time{}, err
	}

	currentVersion := m.currentOpenmanetVersion()

	b, _ := m.board.GetBoard()
	target := ""
	boardName := ""

	if fw, err := m.firmware.GetFirmwareInfo(); err == nil && fw != nil {
		target = fw.Target
	}

	if b != nil {
		boardName = b.Model.ID
	}

	updates := make([]Update, 0, len(releases))

	for _, r := range releases {
		if r.Prerelease && !includePrerelease {
			continue
		}

		// Filter to releases whose tag parses; releases that don't
		// look like semver are not actionable.
		relV, err := ParseTag(r.Tag)
		if err != nil {
			m.log.Debug().Str("tag", r.Tag).Err(err).Msg("dropping release with non-semver tag")

			continue
		}

		// Ask the remote manifest first; fall back to heuristic.
		manifest, _ := m.manifest.FetchManifest(ctx, r)

		asset, err := MatchAsset(boardName, target, manifest, r.Assets)
		if err != nil {
			m.log.Debug().Str("tag", r.Tag).Err(err).Msg("no matching asset for this hardware")

			continue
		}

		newer := false
		if !currentVersion.IsZero() {
			newer = Compare(relV, currentVersion) > 0
		}

		// When current version is unknown, surface every release so
		// the operator can pick (force_install required at upgrade
		// time).
		if !currentVersion.IsZero() && !newer {
			continue
		}

		updates = append(updates, Update{
			Release:          r,
			MatchedAsset:     asset,
			NewerThanCurrent: newer,
		})
	}

	// Newest first.
	sort.Slice(updates, func(i, j int) bool {
		a, _ := ParseTag(updates[i].Release.Tag)
		b, _ := ParseTag(updates[j].Release.Tag)

		return Compare(a, b) > 0
	})

	return updates, fetchedAt, nil
}

// loadReleases returns the cached release list, optionally refreshing
// from GitHub.
func (m *Manager) loadReleases(ctx context.Context, forceRefresh bool) ([]Release, time.Time, error) {
	if !forceRefresh {
		releases, fetchedAt, err := m.cache.Load(ctx)
		if err == nil && len(releases) > 0 {
			return releases, fetchedAt, nil
		}
	}

	m.setPhase(PhaseChecking, "fetching release list from github", "")

	releases, err := m.releases.FetchReleases(ctx)
	if err != nil {
		m.setPhase(PhaseFailed, "github fetch failed", err.Error())

		return nil, time.Time{}, err
	}

	now := time.Now().UTC()
	if err := m.cache.Save(ctx, releases, now); err != nil {
		m.log.Warn().Err(err).Msg("sysupgrade: failed to persist release cache")
	}

	m.setPhase(PhaseIdle, "release list refreshed", "")
	m.markChecked(now)

	return releases, now, nil
}

// GetReleaseDetail returns one release by tag. Reads from the in-memory
// cache; if the cache is empty it triggers a load.
func (m *Manager) GetReleaseDetail(ctx context.Context, tag string) (Release, error) {
	releases, _, err := m.cache.Load(ctx)
	if err != nil {
		return Release{}, err
	}

	if len(releases) == 0 {
		releases, _, err = m.loadReleases(ctx, true)
		if err != nil {
			return Release{}, err
		}
	}

	for _, r := range releases {
		if r.Tag == tag {
			return r, nil
		}
	}

	return Release{}, fmt.Errorf("%w: %s", ErrReleaseNotFound, tag)
}

// StartUpgrade begins the asynchronous Download → Verify → Exec
// sequence. Returns immediately once the goroutine is spawned. Any
// later error surfaces through the progress stream and the manager's
// PhaseFailed state.
func (m *Manager) StartUpgrade(ctx context.Context, tag, assetName string, opts SysupgradeOptions, forceUnknown bool) error {
	if _, err := opts.ToArgs(); err != nil {
		return err
	}

	currentVersion := m.currentOpenmanetVersion()
	if currentVersion.IsZero() && !forceUnknown {
		return ErrUnknownCurrentVersion
	}

	m.mu.Lock()

	if m.progress.Phase == PhaseDownloading || m.progress.Phase == PhaseVerifying || m.progress.Phase == PhaseUpgrading {
		m.mu.Unlock()

		return ErrBusy
	}

	upgradeCtx, cancel := context.WithCancel(context.Background())
	_ = ctx // request ctx is observed only for the synchronous phase; the goroutine has its own lifetime

	m.upgradeCancel = cancel
	m.upgradeStarted = false
	m.mu.Unlock()

	rel, err := m.GetReleaseDetail(upgradeCtx, tag)
	if err != nil {
		cancel()

		return err
	}

	manifest, _ := m.manifest.FetchManifest(upgradeCtx, rel)

	var asset Asset

	for _, a := range rel.Assets {
		if a.Name == assetName {
			asset = a

			break
		}
	}

	if asset.Name == "" {
		// Fall back to MatchAsset so the API may be called with a
		// caller-blank asset_name.
		b, _ := m.board.GetBoard()
		target := ""
		boardName := ""

		if fw, ferr := m.firmware.GetFirmwareInfo(); ferr == nil && fw != nil {
			target = fw.Target
		}

		if b != nil {
			boardName = b.Model.ID
		}

		matched, merr := MatchAsset(boardName, target, manifest, rel.Assets)
		if merr != nil {
			cancel()

			return merr
		}

		asset = matched
	}

	m.wg.Go(func() {
		m.runUpgrade(upgradeCtx, rel, asset, opts)
	})

	return nil
}

// runUpgrade is the goroutine body that performs Download → Verify →
// Exec. It returns once the runner has been called (or earlier on
// error).
func (m *Manager) runUpgrade(ctx context.Context, rel Release, asset Asset, opts SysupgradeOptions) {
	defer func() {
		m.mu.Lock()
		m.upgradeCancel = nil
		m.mu.Unlock()
	}()

	m.setProgress(Progress{
		Phase:      PhaseDownloading,
		ReleaseTag: rel.Tag,
		AssetName:  asset.Name,
		BytesTotal: asset.SizeBytes,
		Message:    "starting download",
		UpdatedAt:  time.Now(),
	})

	imagePath, err := m.downloadAndVerify(ctx, rel, asset)
	if err != nil {
		m.setPhase(PhaseFailed, "download or verify failed", err.Error())

		return
	}

	m.setProgress(Progress{
		Phase:      PhaseReady,
		ReleaseTag: rel.Tag,
		AssetName:  asset.Name,
		Message:    "image verified; launching sysupgrade",
		UpdatedAt:  time.Now(),
	})

	if mkErr := os.MkdirAll(m.downloadDir, 0o755); mkErr != nil {
		m.setPhase(PhaseFailed, "mkdir downloadDir failed", mkErr.Error())

		return
	}

	logPath := filepath.Join(m.downloadDir, asset.Name+".log")

	m.log.Info().
		Str("image", imagePath).
		Str("log", logPath).
		Str("release", rel.Tag).
		Str("asset", asset.Name).
		Msg("sysupgrade: launching detached child for release")

	pid, err := m.runner.Run(ctx, imagePath, logPath, opts)
	if err != nil {
		m.log.Error().Err(err).Str("image", imagePath).Msg("sysupgrade: runner failed to launch")
		m.setPhase(PhaseFailed, "sysupgrade runner failed", err.Error())

		return
	}

	m.mu.Lock()
	m.upgradeStarted = true
	m.mu.Unlock()

	m.log.Info().Int("pid", pid).Str("log", logPath).Msg("sysupgrade: detached child launched")

	m.setProgress(Progress{
		Phase:      PhaseUpgrading,
		ReleaseTag: rel.Tag,
		AssetName:  asset.Name,
		ChildPID:   int32(pid),
		Message:    "sysupgrade running in detached child",
		UpdatedAt:  time.Now(),
	})

	m.wg.Go(func() {
		m.watchSysupgradeChild(ctx, pid, logPath, asset.Name)
	})
}

// downloadAndVerify performs the sums fetch + image stream + verify
// for one asset. The on-disk path of the verified image is returned.
func (m *Manager) downloadAndVerify(ctx context.Context, rel Release, asset Asset) (string, error) {
	expected := ""

	for _, a := range rel.Assets {
		if a.Name == "sha256sums" {
			d, err := FetchSHA256Sum(ctx, m.http, a.DownloadURL, asset.Name)
			if err != nil {
				return "", err
			}

			expected = d

			break
		}
	}

	if expected == "" {
		return "", fmt.Errorf("%w: no sha256sums asset for release %s", ErrChecksumNotFound, rel.Tag)
	}

	dest := filepath.Join(m.downloadDir, asset.Name)

	imagePath, err := streamDownloadVerify(ctx, downloadOptions{
		HTTP:        m.http,
		Asset:       asset,
		DestPath:    dest,
		ExpectedHex: expected,
		ProgressSink: func(p Progress) {
			p.ReleaseTag = rel.Tag
			m.setProgress(p)
		},
	})
	if err != nil {
		return "", err
	}

	m.setProgress(Progress{
		Phase:      PhaseVerifying,
		ReleaseTag: rel.Tag,
		AssetName:  asset.Name,
		Message:    "sha256 verified",
		UpdatedAt:  time.Now(),
	})

	return imagePath, nil
}

// CancelUpgrade stops a running download. Returns
// ErrUpgradeAlreadyExecuting once the runner has returned and the
// detached sysupgrade child is on its own.
func (m *Manager) CancelUpgrade(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.upgradeStarted {
		return ErrUpgradeAlreadyExecuting
	}

	if m.upgradeCancel == nil {
		return ErrNoUpgradeInProgress
	}

	m.upgradeCancel()
	m.upgradeCancel = nil

	m.progress = Progress{
		Phase:     PhaseIdle,
		Message:   "upgrade canceled",
		UpdatedAt: time.Now(),
	}

	m.publishLocked(m.progress)

	return nil
}

// Shutdown cancels any in-flight upgrade context and blocks until every
// goroutine owned by the manager (runUpgrade, runLocalUpgrade, the
// per-upgrade watcher) has returned. Returns ctx.Err() if ctx is
// canceled before the goroutines finish. Safe to call multiple times
// and safe to call when no upgrade is running.
//
// In production, the sysupgrade flow normally ends with the kernel
// killing the daemon mid-flash; Shutdown exists for graceful daemon
// teardown and for tests that need to wait out spawned goroutines
// before tmpdir cleanup runs.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.upgradeCancel != nil {
		m.upgradeCancel()
		m.upgradeCancel = nil
	}
	m.mu.Unlock()

	done := make(chan struct{})

	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("sysupgrade: shutdown wait: %w", ctx.Err())
	}
}

// GetUpgradeStatus returns a copy of the current Progress snapshot.
func (m *Manager) GetUpgradeStatus(_ context.Context) Progress {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.progress
}

// Subscribe registers a buffered receiver for every subsequent Progress
// event. The returned unsubscribe func MUST be deferred by the caller;
// failing to call it leaks an entry in the subscribers map.
func (m *Manager) Subscribe(_ context.Context) (<-chan Progress, func()) {
	ch := make(chan Progress, subscriberQueue)

	m.subsMu.Lock()
	m.nextSub++
	id := m.nextSub
	m.subs[id] = ch
	m.subsMu.Unlock()

	unsub := func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()

		if existing, ok := m.subs[id]; ok {
			delete(m.subs, id)
			close(existing)
		}
	}

	return ch, unsub
}

// setPhase publishes a phase transition with the supplied message and
// optional error. Other Progress fields are inherited from the previous
// snapshot so phase changes don't blank out the in-flight release tag.
func (m *Manager) setPhase(phase Phase, message, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.progress
	m.progress = Progress{
		Phase:      phase,
		ReleaseTag: prev.ReleaseTag,
		AssetName:  prev.AssetName,
		BytesDone:  prev.BytesDone,
		BytesTotal: prev.BytesTotal,
		Percent:    prev.Percent,
		ChildPID:   prev.ChildPID,
		Message:    message,
		ErrMsg:     errMsg,
		UpdatedAt:  time.Now(),
	}

	m.publishLocked(m.progress)
}

// setProgress overwrites the Progress snapshot with the supplied value.
// Used for full updates from the download path; fields not set on the
// caller are zeroed.
func (m *Manager) setProgress(p Progress) {
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.progress = p
	m.publishLocked(p)
}

// publishLocked iterates the subscriber map and non-blocking-sends to
// every channel. m.mu must be held; m.subsMu is acquired internally.
func (m *Manager) publishLocked(p Progress) {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()

	for id, ch := range m.subs {
		select {
		case ch <- p:
		default:
			m.log.Debug().Uint64("sub_id", id).Msg("sysupgrade: subscriber slow, dropping event")
		}
	}
}

// markChecked records the most recent successful github fetch
// timestamp without altering the phase/progress snapshot.
func (m *Manager) markChecked(_ time.Time) {
	// markChecked currently only updates the snapshot consumer (the
	// Progress.UpdatedAt is updated by setPhase). Reserved for future
	// use; the current implementation is intentionally a no-op so the
	// callsite is explicit about its intent.
}

// currentOpenmanetVersion parses the running OpenMANET version from
// /etc/openwrt_release. Returns the zero Version on parse failure;
// callers use IsZero to decide whether to allow the upgrade.
func (m *Manager) currentOpenmanetVersion() Version {
	fw, err := m.firmware.GetFirmwareInfo()
	if err != nil || fw == nil {
		return Version{}
	}

	if fw.OpenMANETVer == "" {
		return Version{}
	}

	v, err := ParseTag(fw.OpenMANETVer)
	if err != nil {
		return Version{}
	}

	return v
}

// snapshotForInstrumentation copies primitive fields into the supplied
// snapshot for the instrumentation framework. The pointer must be
// stable across calls; the framework re-uses the same struct.
func (m *Manager) snapshotForInstrumentation(out *SysupgradeSnapshot) {
	m.mu.Lock()
	prog := m.progress
	started := m.upgradeStarted
	cancel := m.upgradeCancel
	m.mu.Unlock()

	out.Phase = prog.Phase.String()
	out.LastErrorMsg = prog.ErrMsg
	out.InProgress = cancel != nil || started
	out.DownloadedBytes = prog.BytesDone
	out.TotalBytes = prog.BytesTotal
	out.CurrentReleaseTag = prog.ReleaseTag
	out.CurrentAssetName = prog.AssetName
	out.ChildPID = prog.ChildPID

	if cap, err := m.capable.GetSysupgradeCapability(); err == nil && cap != nil {
		out.Capable = cap.Capable
		out.CapableReason = cap.Reason
	}

	if _, fetchedAt, err := m.cache.Load(context.Background()); err == nil {
		out.LastCheckUnix = fetchedAt.Unix()
	}
}
