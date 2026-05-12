package batmanadv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests verify the ingestion-time MAC normalization that the
// package-level parsers apply to batctl JSON output. Handlers rely on
// this contract so they can drop per-request strings.ToLower on every
// originator / neighbor / gateway MAC.

func TestParseOriginators_LowercasesMACs(t *testing.T) {
	raw := []byte(`[
		{"orig_address":"AA:BB:CC:DD:EE:01","best_next_hop":"AA:BB:CC:DD:EE:02","hard_ifname":"wlan0","tq":255,"best":true},
		{"orig_address":"aa:BB:cc:DD:ee:03","best_next_hop":"AA:bb:CC:dd:EE:01","hard_ifname":"wlan0","tq":200,"best":false}
	]`)

	origs, err := parseOriginators(raw)
	require.NoError(t, err)
	require.Len(t, origs, 2)

	assert.Equal(t, "aa:bb:cc:dd:ee:01", origs[0].OrigAddress)
	assert.Equal(t, "aa:bb:cc:dd:ee:02", origs[0].BestNeigh)
	assert.Equal(t, "aa:bb:cc:dd:ee:03", origs[1].OrigAddress)
	assert.Equal(t, "aa:bb:cc:dd:ee:01", origs[1].BestNeigh)
}

func TestParseOriginators_PreservesOtherFields(t *testing.T) {
	raw := []byte(`[{"orig_address":"AA:BB:CC:DD:EE:01","hard_ifname":"PHY2-MESH0","tq":200,"best":true,"last_seen_msecs":120,"throughput":150000}]`)

	origs, err := parseOriginators(raw)
	require.NoError(t, err)
	require.Len(t, origs, 1)

	// Interface name is not a MAC and must not be lower-cased.
	assert.Equal(t, "PHY2-MESH0", origs[0].HardIfname)
	assert.Equal(t, 200, origs[0].TQ)
	assert.InDelta(t, 150000.0, origs[0].Throughput, 0.0001)
	assert.Equal(t, 120, origs[0].LastSeenMs)
	assert.True(t, origs[0].Best)
}

func TestParseGateways_LowercasesMACs(t *testing.T) {
	raw := []byte(`[
		{"orig_address":"AA:BB:CC:DD:EE:01","router":"AA:BB:CC:DD:EE:02","hard_ifname":"wlan0","best":true},
		{"orig_address":"11:22:33:44:55:66","router":"77:88:99:AA:BB:CC","hard_ifname":"wlan0"}
	]`)

	gws, err := parseGateways(raw)
	require.NoError(t, err)
	require.NotNil(t, gws)
	require.Len(t, *gws, 2)

	assert.Equal(t, "aa:bb:cc:dd:ee:01", (*gws)[0].OrigAddress)
	assert.Equal(t, "aa:bb:cc:dd:ee:02", (*gws)[0].Router)
	assert.Equal(t, "11:22:33:44:55:66", (*gws)[1].OrigAddress)
	assert.Equal(t, "77:88:99:aa:bb:cc", (*gws)[1].Router)
}

func TestParseNeighbors_LowercasesMACs(t *testing.T) {
	raw := []byte(`[
		{"neigh_address":"AA:BB:CC:DD:EE:01","hard_ifname":"wlan0","last_seen_msecs":100,"throughput":50},
		{"neigh_address":"aa:BB:cc:DD:ee:02","hard_ifname":"wlan0","last_seen_msecs":200,"throughput":30}
	]`)

	neighs, err := parseNeighbors(raw)
	require.NoError(t, err)
	require.NotNil(t, neighs)
	require.Len(t, *neighs, 2)

	assert.Equal(t, "aa:bb:cc:dd:ee:01", (*neighs)[0].NeighAddress)
	assert.Equal(t, "aa:bb:cc:dd:ee:02", (*neighs)[1].NeighAddress)
}

func TestParseOriginators_EmptyInputReturnsEmptySlice(t *testing.T) {
	origs, err := parseOriginators([]byte(`[]`))
	require.NoError(t, err)
	assert.Empty(t, origs)
}

func TestParseOriginators_InvalidJSONReturnsError(t *testing.T) {
	_, err := parseOriginators([]byte(`not json`))
	assert.Error(t, err)
}
