package handlers_test

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go4.org/mem"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
	"tailscale.com/types/views"

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

	// Verify ConfigureAndEnable was called (it persists config internally)
	assert.Equal(t, 1, mgr.getConfigureAndEnableCalls())
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

// ── new Lattice-panel fields ─────────────────────────────────────────────────

// newPeerStatus builds an *ipnstate.PeerStatus with the supplied fields —
// a lightweight factory that keeps the table-driven tests below readable.
func newPeerStatus(host, dns, relay, curAddr string, online bool, rx, tx int64) *ipnstate.PeerStatus {
	return &ipnstate.PeerStatus{
		HostName: host,
		DNSName:  dns,
		OS:       "linux",
		Relay:    relay,
		CurAddr:  curAddr,
		Online:   online,
		Active:   online,
		RxBytes:  rx,
		TxBytes:  tx,
		TailscaleIPs: []netip.Addr{
			netip.MustParseAddr("100.64.0.10"),
		},
	}
}

func TestGetBLOSStatus_PopulatesIdentityWhenRunning(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")

	self := &ipnstate.PeerStatus{
		HostName: "hazel-gateway",
		DNSName:  "hazel-gateway.tailnet.ts.net.",
		OS:       "linux",
		Relay:    "nyc",
		CurAddr:  "203.0.113.4:41641",
		Tags:     viewOfStrings("tag:gateway"),
		TailscaleIPs: []netip.Addr{
			netip.MustParseAddr("100.64.0.4"),
			netip.MustParseAddr("fd7a:115c::4"),
		},
	}

	mgr := &fakeBLOSManager{
		running: true,
		status: &ipnstate.Status{
			BackendState:   "Running",
			Self:           self,
			CurrentTailnet: &ipnstate.TailnetStatus{Name: "hazel", MagicDNSSuffix: "tailnet.ts.net"},
		},
		prefs: &ipn.Prefs{
			AdvertiseRoutes: []netip.Prefix{netip.MustParsePrefix("10.41.0.0/16")},
		},
		connectedSince: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
		rxBps:          1024,
		txBps:          512,
		rxTotal:        1024 * 60,
		txTotal:        512 * 60,
	}

	svc := newBLOSService(t, cfg, mgr)

	resp, err := svc.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, resp.Identity)
	assert.Equal(t, "hazel-gateway", resp.Identity.Hostname)
	assert.Equal(t, "Running", resp.Identity.BackendState)
	assert.Equal(t, []string{"100.64.0.4", "fd7a:115c::4"}, resp.Identity.OverlayIps)
	assert.Equal(t, "tailnet.ts.net", resp.Identity.MagicDnsSuffix)
	assert.Equal(t, "hazel", resp.Identity.TailnetName)
	require.NotNil(t, resp.Identity.ConnectedSince)
	assert.Equal(t, time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC).Unix(), resp.Identity.ConnectedSince.Seconds)

	require.NotNil(t, resp.Derp)
	assert.Equal(t, "nyc", resp.Derp.Region)
	assert.Equal(t, "direct", resp.Derp.Path)
	assert.Equal(t, "203.0.113.4:41641", resp.Derp.Endpoint)
	require.NotNil(t, resp.Derp.KeepaliveInterval)

	require.NotNil(t, resp.Counters)
	assert.InEpsilon(t, 1024.0, resp.Counters.RxBytesPerSec, 0.0001)
	assert.Equal(t, uint64(1024*60), resp.Counters.RxBytesTotal)

	require.NotNil(t, resp.Network)
	assert.Equal(t, []string{"tag:gateway"}, resp.Network.AclTags)
	assert.Equal(t, []string{"10.41.0.0/16"}, resp.Network.AdvertisedRoutes)
}

func TestGetBLOSStatus_DisabledReturnsBareResponse(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	mgr := &fakeBLOSManager{running: false}
	svc := newBLOSService(t, cfg, mgr)

	resp, err := svc.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Nil(t, resp.Identity)
	assert.Nil(t, resp.Derp)
	assert.Nil(t, resp.Counters)
	assert.Nil(t, resp.Network)
}

func TestListBLOSPeers_ReturnsSortedPeers(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: true\n")

	status := &ipnstate.Status{BackendState: "Running", Peer: map[key.NodePublic]*ipnstate.PeerStatus{}}

	p1 := newPeerStatus("zulu", "zulu.example.", "lhr", "", true, 100, 200)
	p2 := newPeerStatus("alpha", "alpha.example.", "nyc", "198.51.100.9:41641", true, 300, 400)

	status.Peer[stableNodeKey(1)] = p1
	status.Peer[stableNodeKey(2)] = p2

	svc := newBLOSService(t, cfg, &fakeBLOSManager{running: true, status: status})

	resp, err := svc.ListBLOSPeers(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.Peers, 2)

	assert.Equal(t, "alpha", resp.Peers[0].Hostname)
	assert.Equal(t, "direct", resp.Peers[0].Path)
	assert.Equal(t, uint64(300), resp.Peers[0].RxBytes)

	assert.Equal(t, "zulu", resp.Peers[1].Hostname)
	assert.Equal(t, "derp", resp.Peers[1].Path)
}

func TestListBLOSPeers_NotRunningReturnsEmpty(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "blos:\n  enable: false\n")
	svc := newBLOSService(t, cfg, &fakeBLOSManager{running: false})

	resp, err := svc.ListBLOSPeers(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.Peers)
}

// StreamBLOSEvents is covered by the integration tests in
// integration_test.go — a ConnectRPC server stream cannot be constructed
// directly from unit tests because the connect.ServerStream type has no
// public constructor. The integration test drives the stream end-to-end
// through an httptest server and asserts event delivery plus listener
// cleanup on client disconnect.
//
// avoidUnusedImports references a few imports so go vet doesn't complain
// when only table-driven tests reference them.
var _ = connect.CodeInternal
var _ = blos.EventKindPeerAdded

// viewOfStrings wraps a handful of tag strings in the views.Slice form that
// ipnstate.Status uses for Self.Tags. Values are copied so the backing slice
// is owned by the view.
func viewOfStrings(ss ...string) *views.Slice[string] {
	cp := make([]string, len(ss))
	copy(cp, ss)

	v := views.SliceOf(cp)

	return &v
}

// stableNodeKey returns a deterministic key.NodePublic derived from seed.
// Used to populate status.Peer with well-known key values in tests.
func stableNodeKey(seed byte) key.NodePublic {
	var raw [32]byte

	raw[0] = seed

	return key.NodePublicFromRaw32(mem.B(raw[:]))
}
