package blos

import (
	"net/netip"
	"testing"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"
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
