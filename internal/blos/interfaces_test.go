package blos

import (
	"context"
	"net/netip"
	"sync"
	"testing"

	"github.com/digineo/go-uci/v2"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

// TestMTUChainInvariants locks in the on-wire MTU math so a silent regression
// to the previous values (tailscale0=1500, vxlan0=1450) — which ignored
// WireGuard's ~60 byte outer overhead and fragmented on any 1500-MTU internet
// path — would fail the test suite instead of shipping.
func TestMTUChainInvariants(t *testing.T) {
	// vxlanEncapOverhead is the outer L2/L3/L4 overhead introduced when
	// wrapping an inner Ethernet frame in a VXLAN packet:
	//   VXLAN(8) + outer UDP(8) + outer IP(20) + outer Eth(14) → 50B.
	const vxlanEncapOverhead = 50

	// Concrete values. Raising these without re-validating the chain on a
	// measured path would regress the original fix.
	assert.Equal(t, 1280, defaultTunnelDeviceMTUValue,
		"tailscale0 MTU should equal Tailscale's default so we don't fight its PMTUD")
	assert.Equal(t, 1230, vxLanDefaultMTUValue,
		"vxlan0 MTU should subtract the VXLAN encapsulation overhead from the tunnel MTU")

	// Invariant: the overlay MTU must leave room for VXLAN's own overhead
	// on top of whatever the tunnel carries.
	assert.Equal(t, defaultTunnelDeviceMTUValue-vxlanEncapOverhead, vxLanDefaultMTUValue,
		"vxlan0 MTU must equal tunnel MTU minus the VXLAN encapsulation overhead")
}

func TestUpdateTailscalePreferences(t *testing.T) {
	r := &BLOS{logger: zerolog.Nop()}

	prefs := &ipn.Prefs{
		NoSNAT:          true,
		AdvertiseRoutes: nil,
	}

	r.updateTailscalePreferences(prefs)

	// After update, NoSNAT should be false
	assert.False(t, prefs.NoSNAT, "NoSNAT should be disabled")

	// AdvertiseRoutes should contain 10.41.0.0/16
	expectedRoute := netip.MustParsePrefix("10.41.0.0/16")

	require.Len(t, prefs.AdvertiseRoutes, 1)
	assert.Equal(t, expectedRoute, prefs.AdvertiseRoutes[0])
}

func TestUpdateTailscalePreferences_OverwritesExisting(t *testing.T) {
	r := &BLOS{logger: zerolog.Nop()}

	prefs := &ipn.Prefs{
		NoSNAT: true,
		AdvertiseRoutes: []netip.Prefix{
			netip.MustParsePrefix("192.168.0.0/24"),
		},
	}

	r.updateTailscalePreferences(prefs)

	assert.False(t, prefs.NoSNAT)
	require.Len(t, prefs.AdvertiseRoutes, 1)
	assert.Equal(t, netip.MustParsePrefix("10.41.0.0/16"), prefs.AdvertiseRoutes[0])
}

// TestUpdateTailscalePreferences_ConfigOverride verifies that a non-default
// mesh subnet configured via blos.advertisedMeshSubnet is advertised instead
// of the hardcoded default. This is the primary path for deployments whose
// mesh CIDR is not 10.41.0.0/16.
func TestUpdateTailscalePreferences_ConfigOverride(t *testing.T) {
	v := viper.New()
	v.Set("blos.advertisedMeshSubnet", "10.42.0.0/20")

	cfg := config.NewWithoutWatch(v)

	r := &BLOS{logger: zerolog.Nop(), cfg: cfg}

	prefs := &ipn.Prefs{}
	r.updateTailscalePreferences(prefs)

	require.Len(t, prefs.AdvertiseRoutes, 1)
	assert.Equal(t, netip.MustParsePrefix("10.42.0.0/20"), prefs.AdvertiseRoutes[0],
		"BLOS should advertise the CIDR from blos.advertisedMeshSubnet, not the hardcoded default")
}

// TestUpdateTailscalePreferences_ConfigMalformedFallsBack verifies that a
// value that the config loader rejected falls back to the default. The
// loader already substitutes on load, so by the time updateTailscalePreferences
// runs the value is always valid — this test pins that invariant.
func TestUpdateTailscalePreferences_ConfigMalformedFallsBack(t *testing.T) {
	v := viper.New()
	v.Set("blos.advertisedMeshSubnet", "not-a-cidr")

	cfg := config.NewWithoutWatch(v)

	// The loader should have replaced the malformed input with the default.
	assert.Equal(t, config.DefaultBLOSAdvertisedMeshSubnet, cfg.GetBLOSAdvertisedMeshSubnet())

	r := &BLOS{logger: zerolog.Nop(), cfg: cfg}

	prefs := &ipn.Prefs{}
	r.updateTailscalePreferences(prefs)

	require.Len(t, prefs.AdvertiseRoutes, 1)
	assert.Equal(t, netip.MustParsePrefix(config.DefaultBLOSAdvertisedMeshSubnet), prefs.AdvertiseRoutes[0])
}

func TestHasPeerChanges(t *testing.T) {
	tests := []struct {
		lastSynced    map[string]bool
		activePeerIPs map[string]bool
		name          string
		wantChanges   bool
	}{
		{
			name:          "first sync (nil last)",
			lastSynced:    nil,
			activePeerIPs: map[string]bool{"1.2.3.4": true},
			wantChanges:   true,
		},
		{
			name:          "no changes",
			lastSynced:    map[string]bool{"1.2.3.4": true, "5.6.7.8": true},
			activePeerIPs: map[string]bool{"1.2.3.4": true, "5.6.7.8": true},
			wantChanges:   false,
		},
		{
			name:          "peer added",
			lastSynced:    map[string]bool{"1.2.3.4": true},
			activePeerIPs: map[string]bool{"1.2.3.4": true, "5.6.7.8": true},
			wantChanges:   true,
		},
		{
			name:          "peer removed",
			lastSynced:    map[string]bool{"1.2.3.4": true, "5.6.7.8": true},
			activePeerIPs: map[string]bool{"1.2.3.4": true},
			wantChanges:   true,
		},
		{
			name:          "peer replaced",
			lastSynced:    map[string]bool{"1.2.3.4": true},
			activePeerIPs: map[string]bool{"5.6.7.8": true},
			wantChanges:   true,
		},
		{
			name:          "both empty",
			lastSynced:    map[string]bool{},
			activePeerIPs: map[string]bool{},
			wantChanges:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &BLOS{
				logger:            zerolog.Nop(),
				lastSyncedPeerIPs: tt.lastSynced,
			}

			got := r.hasPeerChanges(tt.activePeerIPs)
			assert.Equal(t, tt.wantChanges, got)
		})
	}
}

// fakeUCIReader is a minimal ConfigReader/OpenMANETConfigReader used by the
// configureInterfaces regression test. It only implements the read paths the
// test exercises; all write methods are no-ops that return nil.
type fakeUCIReader struct {
	mu            sync.Mutex
	existingProto map[string]map[string]bool
	options       map[string]map[string]map[string][]string
}

func newFakeUCIReader() *fakeUCIReader {
	return &fakeUCIReader{
		existingProto: make(map[string]map[string]bool),
		options:       make(map[string]map[string]map[string][]string),
	}
}

func (f *fakeUCIReader) addExistingSection(configName, section string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.existingProto[configName] == nil {
		f.existingProto[configName] = make(map[string]bool)
	}

	f.existingProto[configName][section] = true
}

func (f *fakeUCIReader) setOption(configName, section, option, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.options[configName] == nil {
		f.options[configName] = make(map[string]map[string][]string)
	}

	if f.options[configName][section] == nil {
		f.options[configName][section] = make(map[string][]string)
	}

	f.options[configName][section][option] = []string{value}
}

func (f *fakeUCIReader) Get(configName, section, option string) ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if option == "proto" {
		if sections := f.existingProto[configName]; sections != nil && sections[section] {
			return []string{"bridge"}, true
		}
	}

	if cfg := f.options[configName]; cfg != nil {
		if sec := cfg[section]; sec != nil {
			if vals, ok := sec[option]; ok {
				return vals, true
			}
		}
	}

	return nil, false
}

func (f *fakeUCIReader) GetSections(_, _ string) ([]string, error)                   { return nil, nil }
func (f *fakeUCIReader) SetType(_, _, _ string, _ uci.OptionType, _ ...string) error { return nil } //nolint:gofumpt
func (f *fakeUCIReader) Del(_, _, _ string) error                                    { return nil }
func (f *fakeUCIReader) AddSection(_, _, _ string) error                             { return nil }
func (f *fakeUCIReader) DelSection(_, _ string) error                                { return nil }
func (f *fakeUCIReader) Commit() error                                               { return nil }
func (f *fakeUCIReader) ReloadConfig() error                                         { return nil }

// runningTailscaleClient reports a Running backend state and no-op prefs.
type runningTailscaleClient struct{}

func (*runningTailscaleClient) Status(_ context.Context) (*ipnstate.Status, error) {
	return &ipnstate.Status{BackendState: "Running"}, nil
}

func (*runningTailscaleClient) GetPrefs(_ context.Context) (*ipn.Prefs, error) {
	return &ipn.Prefs{}, nil
}

func (*runningTailscaleClient) EditPrefs(_ context.Context, _ *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	return &ipn.Prefs{}, nil
}

// TestBLOS_ConfigureInterfaces_ReturnsAfterReboot guards the fix for the
// blos.enable persistence bug: on a first-time Enable the !blosConfigured
// branch commits UCI and calls reboot(). Before the fix, control fell through
// to the post-reboot SetMTU chain on tailscale0/vxlan0 — interfaces that only
// exist after the pending reboot has brought up the new UCI config — so
// SetMTU returned "interface not found". That error bubbled up to
// ConfigureAndEnable, which rolled blos.enable back to false; after the reboot
// the daemon then read blos.enable=false and BLOS stayed off. This test pins
// the invariant that configureInterfaces MUST NOT run SetMTU after kicking off
// the reboot.
func TestBLOS_ConfigureInterfaces_ReturnsAfterReboot(t *testing.T) {
	v := viper.New()
	v.Set("alfred.batInterface", "bat0")

	cfg := config.NewWithoutWatch(v)

	netReader := newFakeUCIReader()
	// Mesh bat interface must exist so configureInterfaces enters the outer branch.
	netReader.addExistingSection("network", "bat0")
	// Make tunnel/vxlan/batman look already-present in UCI so the
	// create-or-configure helpers short-circuit to nil — this test is not
	// exercising their internals.
	netReader.addExistingSection("network", "tailscale0")
	netReader.addExistingSection("network", "vxlan0")
	netReader.addExistingSection("network", "battunnel0")

	ompReader := newFakeUCIReader()
	// BLOSconfigured="0" drives configureInterfaces into the !blosConfigured
	// branch, which is the branch that calls reboot().
	ompReader.setOption("openmanetd", "config", "BLOSconfigured", "0")

	var (
		rebootCalls int
		setMTUCalls int
	)

	r := &BLOS{
		cfg:                cfg,
		logger:             zerolog.Nop(),
		tsClient:           &runningTailscaleClient{},
		uciNetworkConfig:   netReader,
		uciOpenManetConfig: ompReader,
		uciFirewallConfig:  newFakeUCIReader(),
		Reboot: func() error {
			rebootCalls++

			return nil
		},
		SetMTU: func(_ string, _ int) error {
			setMTUCalls++

			return nil
		},
	}

	err := r.configureInterfaces(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, rebootCalls, "reboot must be invoked on first-time configuration")
	assert.Equal(t, 0, setMTUCalls,
		"SetMTU must not run after reboot — tunnel/vxlan are not yet up; "+
			"a SetMTU error here would trigger a config rollback and silently disable BLOS")
}
