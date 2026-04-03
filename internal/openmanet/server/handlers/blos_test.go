package handlers_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
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

func newBLOSService(t *testing.T, cfg *config.Config, mgr *fakeBLOSManager) *handlers.BLOSService {
	t.Helper()

	return &handlers.BLOSService{
		Cfg:         cfg,
		Log:         zerolog.Nop(),
		BLOSManager: mgr,
	}
}

// ── GetBLOSStatus ─────────────────────────────────────────────────────────────

func TestGetBLOSStatus_Enabled(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")
	mgr := &fakeBLOSManager{running: true}
	svc := newBLOSService(t, cfg, mgr)

	resp, err := svc.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.BlosEnabled)
	assert.Contains(t, resp.GetMessage(), "enabled and running")
}

func TestGetBLOSStatus_Disabled(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{running: false}
	svc := newBLOSService(t, cfg, mgr)

	resp, err := svc.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.False(t, resp.BlosEnabled)
	assert.Contains(t, resp.GetMessage(), "disabled")
}

func TestGetBLOSStatus_ConfigEnabledButNotRunning(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")
	mgr := &fakeBLOSManager{running: false}
	svc := newBLOSService(t, cfg, mgr)

	resp, err := svc.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.BlosEnabled)
	assert.Contains(t, resp.GetMessage(), "not currently running")
}

// ── UpdateBLOSConfig (enable) ─────────────────────────────────────────────────

func TestUpdateBLOSConfig_Enable_Success(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	svc := newBLOSService(t, cfg, mgr)

	resp, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "tskey-abc123",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Verify ConfigureAndEnable was called
	assert.Equal(t, 1, mgr.getConfigureAndEnableCalls())

	// Verify config persisted
	assert.True(t, cfg.BLOSEnabled())
}

func TestUpdateBLOSConfig_Enable_WithLoginServer(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	svc := newBLOSService(t, cfg, mgr)

	loginURL := "https://hs.example.com"
	resp, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos:     true,
		AuthKey:        "tskey-abc123",
		LoginServerUrl: &loginURL,
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)

	// ConfigureAndEnable handles login server internally
	assert.Equal(t, 1, mgr.getConfigureAndEnableCalls())
}

func TestUpdateBLOSConfig_Enable_EmptyAuthKey(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	svc := newBLOSService(t, cfg, mgr)

	_, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_key is required")

	// Nothing should have been called
	assert.Equal(t, 0, mgr.getConfigureAndEnableCalls())
	assert.False(t, cfg.BLOSEnabled())
}

func TestUpdateBLOSConfig_Enable_WhitespaceAuthKey(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	svc := newBLOSService(t, cfg, mgr)

	_, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "   ",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_key is required")
	assert.Equal(t, 0, mgr.getConfigureAndEnableCalls())
}

func TestUpdateBLOSConfig_Enable_ConfigureAndEnableFailure(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{
		configureAndEnableErr: errors.New("tailscale authentication failed"),
	}
	svc := newBLOSService(t, cfg, mgr)

	_, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "tskey-bad",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to enable BLOS")

	// Config should NOT be updated
	assert.False(t, cfg.BLOSEnabled())
}

// ── UpdateBLOSConfig (disable) ────────────────────────────────────────────────

func TestUpdateBLOSConfig_Disable_Success(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")
	mgr := &fakeBLOSManager{running: true}
	svc := newBLOSService(t, cfg, mgr)

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

	// No ConfigureAndEnable needed for disable
	assert.Equal(t, 0, mgr.getConfigureAndEnableCalls())
}

func TestUpdateBLOSConfig_Disable_AlreadyDisabled(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{running: false}
	svc := newBLOSService(t, cfg, mgr)

	resp, err := svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: false,
		AuthKey:    "",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 1, mgr.getDisableCalls()) // still called, idempotent
}

// ── Rollback on persist failure ──────────────────────────────────────────────

func TestUpdateBLOSConfig_Enable_PersistFailure_RollsBack(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	svc := newBLOSService(t, cfg, mgr)

	// Make the config file read-only so PersistBLOSConfig fails
	err := os.Chmod(cfg.GetConfigFilePath(), 0444)
	require.NoError(t, err)

	_, err = svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "tskey-abc123",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rolled back")

	// ConfigureAndEnable was called, then rolled back via Disable
	assert.Equal(t, 1, mgr.getConfigureAndEnableCalls())
	assert.Equal(t, 1, mgr.getDisableCalls())
	assert.False(t, mgr.IsRunning())
}

func TestUpdateBLOSConfig_Disable_PersistFailure_RollsBack(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")
	mgr := &fakeBLOSManager{running: true}
	svc := newBLOSService(t, cfg, mgr)

	// Make the config file read-only so PersistBLOSConfig fails
	err := os.Chmod(cfg.GetConfigFilePath(), 0444)
	require.NoError(t, err)

	_, err = svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
		EnableBlos: false,
		AuthKey:    "",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rolled back")

	// Disable was called, then Enable was called to roll back
	assert.Equal(t, 1, mgr.getDisableCalls())
	assert.Equal(t, 1, mgr.getEnableCalls())
	assert.True(t, mgr.IsRunning())
}

// ── Concurrent enable/disable ────────────────────────────────────────────────

func TestUpdateBLOSConfig_ConcurrentEnableDisable(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{}
	svc := newBLOSService(t, cfg, mgr)

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < 20; j++ {
				if j%2 == 0 {
					_, _ = svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
						EnableBlos: true,
						AuthKey:    "tskey-test",
					})
				} else {
					_, _ = svc.UpdateBLOSConfig(context.Background(), &v1.UpdateBLOSConfigRequest{
						EnableBlos: false,
					})
				}
			}
		}()
	}

	wg.Wait()
	// If we get here without panic or race, the test passes.
}
