package handlers_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	supbv1 "github.com/openmanet/openmanetd/internal/api/openmanet/sysupgrade/v1"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/openmanet/openmanetd/internal/sysupgrade"
)

// fakeSysupgradeManager is a hand-written fake implementing the
// SysupgradeManager interface. All mutable state is protected by mu.
type fakeSysupgradeManager struct {
	mu              sync.Mutex
	systemInfo      *sysupgrade.SystemInfo
	updates         []sysupgrade.Update
	fetchedAt       time.Time
	release         sysupgrade.Release
	progress        sysupgrade.Progress
	staged          *sysupgrade.StagedImage
	startErr        error
	cancelErr       error
	infoErr         error
	updatesErr      error
	releaseErr      error
	discardErr      error
	startLocalErr   error
	startCalls      int
	discardCalls    int
	startLocalCalls int
	startLocalOpts  sysupgrade.SysupgradeOptions
	startLocalSkip  bool
	startLocalForce bool
	startCh         chan sysupgrade.Progress
}

func newFakeSysupgradeManager() *fakeSysupgradeManager {
	return &fakeSysupgradeManager{
		systemInfo: &sysupgrade.SystemInfo{Hostname: "h", Target: "bcm27xx/bcm2711"},
		progress:   sysupgrade.Progress{Phase: sysupgrade.PhaseIdle, UpdatedAt: time.Now()},
		startCh:    make(chan sysupgrade.Progress, 4),
	}
}

func (f *fakeSysupgradeManager) GetSystemInfo(_ context.Context) (*sysupgrade.SystemInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.systemInfo, f.infoErr
}

func (f *fakeSysupgradeManager) ListAvailableUpdates(_ context.Context, _, _ bool) ([]sysupgrade.Update, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.updates, f.fetchedAt, f.updatesErr
}

func (f *fakeSysupgradeManager) GetReleaseDetail(_ context.Context, _ string) (sysupgrade.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.release, f.releaseErr
}

func (f *fakeSysupgradeManager) StartUpgrade(_ context.Context, _, _ string, _ sysupgrade.SysupgradeOptions, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.startCalls++

	return f.startErr
}

func (f *fakeSysupgradeManager) CancelUpgrade(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.cancelErr
}

func (f *fakeSysupgradeManager) GetUpgradeStatus(_ context.Context) sysupgrade.Progress {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.progress
}

func (f *fakeSysupgradeManager) Subscribe(_ context.Context) (<-chan sysupgrade.Progress, func()) {
	return f.startCh, func() {}
}

func (f *fakeSysupgradeManager) GetStagedImage() *sysupgrade.StagedImage {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.staged == nil {
		return nil
	}

	out := *f.staged

	return &out
}

func (f *fakeSysupgradeManager) DiscardStagedImage() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.discardCalls++

	if f.discardErr != nil {
		return f.discardErr
	}

	f.staged = nil

	return nil
}

func (f *fakeSysupgradeManager) StartLocalUpgrade(_ context.Context, opts sysupgrade.SysupgradeOptions, skipPreflight, forceUnknown bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.startLocalCalls++
	f.startLocalOpts = opts
	f.startLocalSkip = skipPreflight
	f.startLocalForce = forceUnknown

	return f.startLocalErr
}

func (f *fakeSysupgradeManager) getDiscardCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.discardCalls
}

func (f *fakeSysupgradeManager) getStartLocalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.startLocalCalls
}

func (f *fakeSysupgradeManager) getStartCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.startCalls
}

// ─── handler tests ─────────────────────────────────────────────────────

func TestSysupgradeService_GetSystemInfo(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	resp, err := svc.GetSystemInfo(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "h", resp.GetInfo().GetHostname())
	assert.Equal(t, "bcm27xx/bcm2711", resp.GetInfo().GetTarget())
}

func TestSysupgradeService_StartUpgrade_Conflict(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	mgr.startErr = sysupgrade.ErrOptionConflict
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	_, err := svc.StartUpgrade(context.Background(), &supbv1.StartUpgradeRequest{
		ReleaseTag: "v1.8.0",
		AssetName:  "x",
	})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
	assert.Equal(t, 1, mgr.getStartCalls())
}

func TestSysupgradeService_StartUpgrade_UnknownVersion(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	mgr.startErr = sysupgrade.ErrUnknownCurrentVersion
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	_, err := svc.StartUpgrade(context.Background(), &supbv1.StartUpgradeRequest{
		ReleaseTag: "v1.8.0",
	})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
}

func TestSysupgradeService_StartUpgrade_OK(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	_, err := svc.StartUpgrade(context.Background(), &supbv1.StartUpgradeRequest{
		ReleaseTag: "v1.8.0",
		AssetName:  "x",
		Options:    &supbv1.SysupgradeOptions{TestOnly: true},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, mgr.getStartCalls())
}

func TestSysupgradeService_GetUpgradeStatus(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	mgr.progress = sysupgrade.Progress{
		Phase:      sysupgrade.PhaseDownloading,
		Percent:    42,
		BytesDone:  100,
		BytesTotal: 200,
		ReleaseTag: "v1.8.0",
		UpdatedAt:  time.Now(),
	}

	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	resp, err := svc.GetUpgradeStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	ev := resp.GetEvent()
	assert.Equal(t, supbv1.Phase_PHASE_DOWNLOADING, ev.GetPhase())
	assert.Equal(t, int32(42), ev.GetPercent())
	assert.Equal(t, "v1.8.0", ev.GetReleaseTag())
}

func TestSysupgradeService_GetStagedImage_Empty(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	resp, err := svc.GetStagedImage(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Nil(t, resp.GetImage())
}

func TestSysupgradeService_GetStagedImage_Populated(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	mgr.staged = &sysupgrade.StagedImage{
		Filename:              "openmanet-bcm27xx.img.gz",
		SizeBytes:             4096,
		Sha256:                "abcdef",
		FilenameMatchesTarget: true,
		PreflightOK:           true,
		UploadedAt:            time.Now(),
	}

	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	resp, err := svc.GetStagedImage(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	img := resp.GetImage()
	require.NotNil(t, img)
	assert.Equal(t, "openmanet-bcm27xx.img.gz", img.GetFilename())
	assert.Equal(t, int64(4096), img.GetSizeBytes())
	assert.Equal(t, "abcdef", img.GetSha256())
	assert.True(t, img.GetFilenameMatchesTarget())
	assert.True(t, img.GetPreflightOk())
	assert.NotNil(t, img.GetUploadedAt())
}

func TestSysupgradeService_DiscardStagedImage_Idempotent(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	mgr.discardErr = sysupgrade.ErrNoStagedImage
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	_, err := svc.DiscardStagedImage(context.Background(), &emptypb.Empty{})
	require.NoError(t, err, "no-image-staged is treated as success")
	assert.Equal(t, 1, mgr.getDiscardCalls())
}

func TestSysupgradeService_DiscardStagedImage_OK(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	mgr.staged = &sysupgrade.StagedImage{Filename: "a.img"}
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	_, err := svc.DiscardStagedImage(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, 1, mgr.getDiscardCalls())
}

func TestSysupgradeService_StartLocalUpgrade_OK(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	_, err := svc.StartLocalUpgrade(context.Background(), &supbv1.StartLocalUpgradeRequest{
		Options:                    &supbv1.SysupgradeOptions{Verbose: true},
		ForceInstallUnknownCurrent: true,
		SkipPreflight:              true,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, mgr.getStartLocalCalls())
	assert.True(t, mgr.startLocalForce)
	assert.True(t, mgr.startLocalSkip)
	assert.True(t, mgr.startLocalOpts.Verbose)
}

func TestSysupgradeService_StartLocalUpgrade_PreflightFailed(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	mgr.startLocalErr = sysupgrade.ErrStagedPreflightFailed
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	_, err := svc.StartLocalUpgrade(context.Background(), &supbv1.StartLocalUpgradeRequest{})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
}

func TestSysupgradeService_StartLocalUpgrade_NoImage(t *testing.T) {
	mgr := newFakeSysupgradeManager()
	mgr.startLocalErr = sysupgrade.ErrNoStagedImage
	svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}

	_, err := svc.StartLocalUpgrade(context.Background(), &supbv1.StartLocalUpgradeRequest{})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
}

func TestSysupgradeService_CancelUpgrade_Errors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code connect.Code
	}{
		{name: "no upgrade", err: sysupgrade.ErrNoUpgradeInProgress, code: connect.CodeFailedPrecondition},
		{name: "already executing", err: sysupgrade.ErrUpgradeAlreadyExecuting, code: connect.CodeFailedPrecondition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := newFakeSysupgradeManager()
			mgr.cancelErr = tt.err

			svc := &handlers.SysupgradeService{Log: zerolog.Nop(), Manager: mgr}
			_, err := svc.CancelUpgrade(context.Background(), &emptypb.Empty{})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.Equal(t, tt.code, connectErr.Code())
		})
	}
}
