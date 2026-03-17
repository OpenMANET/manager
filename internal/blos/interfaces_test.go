package blos

import (
	"net/netip"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"
)

func TestContainsString(t *testing.T) {
	tests := []struct {
		name   string
		items  []string
		target string
		want   bool
	}{
		{
			name:   "found in middle",
			items:  []string{"a", "b", "c"},
			target: "b",
			want:   true,
		},
		{
			name:   "found at start",
			items:  []string{"x", "y", "z"},
			target: "x",
			want:   true,
		},
		{
			name:   "found at end",
			items:  []string{"x", "y", "z"},
			target: "z",
			want:   true,
		},
		{
			name:   "not found",
			items:  []string{"a", "b", "c"},
			target: "d",
			want:   false,
		},
		{
			name:   "empty slice",
			items:  []string{},
			target: "a",
			want:   false,
		},
		{
			name:   "nil slice",
			items:  nil,
			target: "a",
			want:   false,
		},
		{
			name:   "single item match",
			items:  []string{"only"},
			target: "only",
			want:   true,
		},
		{
			name:   "single item no match",
			items:  []string{"only"},
			target: "other",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsString(tt.items, tt.target)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUpdateTailscalePreferences(t *testing.T) {
	r := &BLOS{Logger: zerolog.Nop()}

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
	r := &BLOS{Logger: zerolog.Nop()}

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

func TestHasPeerChanges(t *testing.T) {
	tests := []struct {
		name          string
		lastSynced    map[string]bool
		activePeerIPs map[string]bool
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
				Logger:            zerolog.Nop(),
				lastSyncedPeerIPs: tt.lastSynced,
			}

			got := r.hasPeerChanges(tt.activePeerIPs)
			assert.Equal(t, tt.wantChanges, got)
		})
	}
}
