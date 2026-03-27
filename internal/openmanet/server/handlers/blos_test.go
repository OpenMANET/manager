package handlers_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

// setupBLOSTestConfig creates a Config backed by a temp YAML file for handler tests.
// It does NOT start the file watcher to avoid race conditions in tests.
func setupBLOSTestConfig(t *testing.T, yamlContent string) *config.Config {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)

	err = v.ReadInConfig()
	require.NoError(t, err)

	return config.NewWithoutWatch(v)
}

func newBLOSService(t *testing.T, cfg *config.Config, mgr *fakeBLOSManager, cmd *fakeRunCommand) *handlers.BLOSService {
	t.Helper()

	return &handlers.BLOSService{
		Cfg:         cfg,
		Log:         zerolog.Nop(),
		BLOSManager: mgr,
		RunCommand:  cmd.Run,
	}
}

// ── GetBLOSStatus ─────────────────────────────────────────────────────────────

func TestGetBLOSStatus_Enabled(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")
	mgr := &fakeBLOSManager{running: true}
	svc := newBLOSService(t, cfg, mgr, &fakeRunCommand{})

	resp, err := svc.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.BlosEnabled)
	assert.Contains(t, resp.GetMessage(), "enabled and running")
}

func TestGetBLOSStatus_Disabled(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{running: false}
	svc := newBLOSService(t, cfg, mgr, &fakeRunCommand{})

	resp, err := svc.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.False(t, resp.BlosEnabled)
	assert.Contains(t, resp.GetMessage(), "disabled")
}

func TestGetBLOSStatus_ConfigEnabledButNotRunning(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")
	mgr := &fakeBLOSManager{running: false}
	svc := newBLOSService(t, cfg, mgr, &fakeRunCommand{})

	resp, err := svc.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.BlosEnabled)
	assert.Contains(t, resp.GetMessage(), "not currently running")
}

// ── UpdateBLOSConfig (enable) ─────────────────────────────────────────────────

func TestUpdateBLOSConfig_Enable_Success(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	cmd := &fakeRunCommand{}
	svc := newBLOSService(t, cfg, mgr, cmd)

	resp, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "tskey-abc123",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Verify tailscale was called correctly
	calls := cmd.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "tailscale", calls[0][0])
	assert.Contains(t, calls[0], "up")
	assert.Contains(t, calls[0], "--authkey=tskey-abc123")

	// Verify config persisted
	assert.True(t, cfg.BLOSEnabled())

	// Verify manager enabled
	assert.Equal(t, 1, mgr.getEnableCalls())
}

func TestUpdateBLOSConfig_Enable_WithLoginServer(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	cmd := &fakeRunCommand{}
	svc := newBLOSService(t, cfg, mgr, cmd)

	loginURL := "https://hs.example.com"
	resp, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos:     true,
		AuthKey:        "tskey-abc123",
		LoginServerUrl: &loginURL,
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)

	calls := cmd.getCalls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0], "--login-server=https://hs.example.com")
}

func TestUpdateBLOSConfig_Enable_EmptyAuthKey(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	cmd := &fakeRunCommand{}
	svc := newBLOSService(t, cfg, mgr, cmd)

	_, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_key is required")

	// Nothing should have been called
	assert.Equal(t, 0, cmd.callCount())
	assert.Equal(t, 0, mgr.getEnableCalls())
	assert.False(t, cfg.BLOSEnabled())
}

func TestUpdateBLOSConfig_Enable_WhitespaceAuthKey(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	cmd := &fakeRunCommand{}
	svc := newBLOSService(t, cfg, mgr, cmd)

	_, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "   ",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_key is required")
	assert.Equal(t, 0, cmd.callCount())
}

func TestUpdateBLOSConfig_Enable_TailscaleFailure(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	cmd := &fakeRunCommand{
		output: []byte("error: not logged in"),
		err:    errors.New("exit status 1"),
	}
	svc := newBLOSService(t, cfg, mgr, cmd)

	_, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "tskey-bad",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tailscale authentication failed")

	// Config should NOT be updated
	assert.False(t, cfg.BLOSEnabled())

	// Manager should NOT be called
	assert.Equal(t, 0, mgr.getEnableCalls())
}

func TestUpdateBLOSConfig_Enable_ManagerEnableFailure(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{enableErr: errors.New("not gateway mode")}
	cmd := &fakeRunCommand{}
	svc := newBLOSService(t, cfg, mgr, cmd)

	_, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "tskey-abc123",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "BLOS module failed to start")

	// Tailscale was called (succeeded)
	assert.Equal(t, 1, cmd.callCount())

	// Config was persisted (intentional: next restart will retry)
	assert.True(t, cfg.BLOSEnabled())
}

// ── UpdateBLOSConfig (disable) ────────────────────────────────────────────────

func TestUpdateBLOSConfig_Disable_Success(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")
	mgr := &fakeBLOSManager{running: true}
	cmd := &fakeRunCommand{}
	svc := newBLOSService(t, cfg, mgr, cmd)

	resp, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: false,
		AuthKey:    "ignored-for-disable",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Manager should be disabled
	assert.Equal(t, 1, mgr.getDisableCalls())

	// Config should be updated
	assert.False(t, cfg.BLOSEnabled())

	// No tailscale command needed for disable
	assert.Equal(t, 0, cmd.callCount())
}

func TestUpdateBLOSConfig_Disable_AlreadyDisabled(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{running: false}
	cmd := &fakeRunCommand{}
	svc := newBLOSService(t, cfg, mgr, cmd)

	resp, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: false,
		AuthKey:    "",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 1, mgr.getDisableCalls()) // still called, idempotent
}
