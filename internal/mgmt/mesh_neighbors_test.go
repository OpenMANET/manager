package mgmt

import (
	"net"
	"testing"
	"time"

	netv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeshNeighborsWorker_SendOncePublishesExpectedPayload asserts the
// publisher exec's batctl wrappers, maps every neighbor's hard_ifname
// faithfully (including the pre-computed blos flag), and filters the
// originator table down to Best==true rows.
func TestMeshNeighborsWorker_SendOncePublishesExpectedPayload(t *testing.T) {
	fixedTime := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	neighbors := batmanadv.Neighbors{
		{HardIfname: "wlh0", NeighAddress: "AA:AA:AA:AA:AA:01", LastSeenMsecs: 200, Throughput: 54000},
		{HardIfname: "vxlan0", NeighAddress: "bb:bb:bb:bb:bb:01", LastSeenMsecs: 400, Throughput: 100000},
		{HardIfname: "eth0", NeighAddress: "Cc:Cc:Cc:Cc:Cc:01", LastSeenMsecs: 50, Throughput: 1000000},
	}

	originators := []batmanadv.Originator{
		{OrigAddress: "aa:aa:aa:aa:aa:01", BestNeigh: "aa:aa:aa:aa:aa:01", HardIfname: "wlh0", Throughput: 54000, TQ: 220, Best: true},
		// Non-best row — must be filtered out.
		{OrigAddress: "aa:aa:aa:aa:aa:01", BestNeigh: "dd:dd:dd:dd:dd:01", HardIfname: "wlh0", Throughput: 20000, TQ: 120, Best: false},
		{OrigAddress: "bb:bb:bb:bb:bb:01", BestNeigh: "bb:bb:bb:bb:bb:01", HardIfname: "vxlan0", Throughput: 100000, TQ: 0, Best: true},
	}

	meshCfg := &batmanadv.MeshConfig{AlgoName: "BATMAN_V"}

	fake := &fakeAlfredClient{}

	worker := &MeshNeighborsWorker{
		Config: &ManagementConfig{
			Log:          zerolog.Nop(),
			BatInterface: "bat0",
		},
		Interval: time.Second,
		Now:      func() time.Time { return fixedTime },
	}

	err := worker.sendOnceWithDeps(
		fake,
		func() (*batmanadv.Neighbors, error) { return &neighbors, nil },
		func() ([]batmanadv.Originator, error) { return originators, nil },
		func() (*batmanadv.MeshConfig, error) { return meshCfg, nil },
		func(name string) network.NetworkInterface {
			require.Equal(t, "bat0", name)

			return network.NetworkInterface{MAC: "11:22:33:44:55:66"}
		},
		func() (string, error) { return "BCM2711-1003_bat0", nil },
		func() []string {
			return []string{"11:22:33:44:55:66", "ff:ee:dd:cc:bb:aa", "fe:fe:fe:fe:fe:01"}
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, fake.setCalls, "exactly one publish per tick")

	got := &netv1.MeshNeighbors{}
	require.NoError(t, got.UnmarshalVT(fake.lastData))

	assert.Equal(t, "11:22:33:44:55:66", got.GetPrimaryMac())
	assert.Equal(t, "BCM2711-1003", got.GetHostname(), "iface suffix stripped")
	assert.Equal(t, int32(15), got.GetAlgorithm(), "BATMAN_V → 15")
	assert.Equal(t, fixedTime.Unix(), got.GetCollectedAt().GetSeconds())

	require.Len(t, got.GetNeighbors(), 3)

	byMac := map[string]*netv1.MeshNeighbor{}
	for _, n := range got.GetNeighbors() {
		byMac[n.GetMac()] = n
	}

	wlan, ok := byMac["aa:aa:aa:aa:aa:01"]
	require.True(t, ok)
	assert.Equal(t, "wlh0", wlan.GetHardIfname())
	assert.False(t, wlan.GetBlos(), "wlh0 is not BLOS")
	assert.Equal(t, int32(200), wlan.GetLastSeenMsecs())
	assert.Equal(t, int64(54000), wlan.GetThroughputKbps())

	vxlan, ok := byMac["bb:bb:bb:bb:bb:01"]
	require.True(t, ok)
	assert.Equal(t, "vxlan0", vxlan.GetHardIfname())
	assert.True(t, vxlan.GetBlos(), "vxlan0 is the BLOS interface")

	_, ok = byMac["cc:cc:cc:cc:cc:01"]
	assert.True(t, ok, "mixed-case MACs are normalized to lowercase")

	// Originators: only the two Best==true rows survive.
	require.Len(t, got.GetOriginators(), 2)

	origByMac := map[string]*netv1.Originator{}
	for _, o := range got.GetOriginators() {
		origByMac[o.GetOrigMac()] = o
	}

	require.Contains(t, origByMac, "aa:aa:aa:aa:aa:01")
	assert.Equal(t, int32(220), origByMac["aa:aa:aa:aa:aa:01"].GetTq())
	assert.Equal(t, "wlh0", origByMac["aa:aa:aa:aa:aa:01"].GetHardIfname())

	// InterfaceMacs include the bat0 primary plus every MAC the
	// listMacs hook returned, deduped and lowercased. Receivers index
	// records by every entry so a Lookup keyed on any of them resolves.
	assert.ElementsMatch(t,
		[]string{"11:22:33:44:55:66", "ff:ee:dd:cc:bb:aa", "fe:fe:fe:fe:fe:01"},
		got.GetInterfaceMacs())
}

// TestStripIfaceSuffix exercises the regex against every bat-hosts
// suffix shape that appears in testfixtures/batman-adv/bat-hosts plus
// the regression cases (Gate_04_27 must survive, the chained
// "_phy2-mesh0_bat0" must collapse in one match).
func TestStripIfaceSuffix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Canonical iface suffixes from the production bat-hosts.
		{"BCM2711-97d6_bat0", "BCM2711-97d6"},
		{"BCM2711-97d6_wlh0", "BCM2711-97d6"},
		{"BCM2711-97d6_phy2-mesh0", "BCM2711-97d6"},
		{"BCM2711-97d6_eth0", "BCM2711-97d6"},
		{"BCM2711-97d6_wlh-97-d9", "BCM2711-97d6"},
		{"BCM2711-88ba_br-ahwlan", "BCM2711-88ba"},
		{"BCM2711-1003_wlh-10-04", "BCM2711-1003"},
		{"BCM2711-1003_phy1-mesh0", "BCM2711-1003"},
		{"HaLow-R-b65c57_phy0-mesh0", "HaLow-R-b65c57"},
		{"HaLow-R-b65c57_mesh0", "HaLow-R-b65c57"},
		{"BLOS-GW1_vxlan0", "BLOS-GW1"},
		{"Remote-Node1_vxlan0", "Remote-Node1"},
		// Chained suffix — single match consumes both because `_` is in
		// the iface character class.
		{"BCM2711-88ba_phy2-mesh0_bat0", "BCM2711-88ba"},
		// Legitimate underscored hostnames must survive.
		{"Gate_04_27", "Gate_04_27"},
		{"Gate_04", "Gate_04"},
		// Uppercase and digit-starting suffixes are not iface names.
		{"node_BACKUP", "node_BACKUP"},
		{"node_27", "node_27"},
		// Degenerate inputs.
		{"plain", "plain"},
		{"", ""},
		{"_bat0", "_bat0"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, stripIfaceSuffix(tc.in), "stripIfaceSuffix(%q)", tc.in)
	}
}

// TestIsUsableInterfaceMac pins the bit-level rejection rules. The
// driver-default MAC 12:00:00:00:00:00 (Morse Micro HaLow radio's
// hard-coded address — same on every device) MUST be rejected so it
// never ends up in interface_macs.
func TestIsUsableInterfaceMac(t *testing.T) {
	cases := []struct {
		name  string
		input net.HardwareAddr
		want  bool
	}{
		{"empty", net.HardwareAddr{}, false},
		{"too short", net.HardwareAddr{0x02, 0x03, 0x04}, false},
		{"all zero", net.HardwareAddr{0, 0, 0, 0, 0, 0}, false},
		{"multicast bit set", net.HardwareAddr{0x01, 0x00, 0x5e, 0x00, 0x01, 0x01}, false},
		{"morse0 placeholder 12:00:00:00:00:00", net.HardwareAddr{0x12, 0x00, 0x00, 0x00, 0x00, 0x00}, false},
		{"trailing zeros 02:00:00:00:00:00", net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x00}, false},
		{"locally-administered with real lower bytes", net.HardwareAddr{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc}, true},
		{"globally-unique unicast", net.HardwareAddr{0x3c, 0x22, 0x7f, 0x37, 0x4c, 0x0c}, true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, isUsableInterfaceMac(tc.input), "input %v", tc.input)
	}
}

// TestIsNonMeshInterfaceName asserts the name-based skip catches
// every prefix the snapshot publisher must ignore — most importantly
// morse0, the HaLow radio whose driver assigns the same shared MAC
// to every device. Tunnel prefixes are belt-and-suspenders alongside
// the FlagPointToPoint filter.
func TestIsNonMeshInterfaceName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"morse0", true},
		{"morse1", true},
		{"MORSE0", true}, // case-insensitive
		{"tun0", true},
		{"tap0", true},
		{"wg0", true},
		{"tailscale0", true},
		{"ppp0", true},
		{"docker0", true},
		// Mesh-relevant interfaces must NOT be filtered.
		{"bat0", false},
		{"vxlan0", false},
		{"wlh0", false},
		{"phy2-mesh0", false},
		{"eth0", false},
		{"br-ahwlan", false},
		{"mesh0", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, isNonMeshInterfaceName(tc.name), "name %q", tc.name)
	}
}

// TestCollectInterfaceMacs covers the publisher's mac-bag composition:
// always include primary, accept nil listMacs, dedupe, lowercase, sort.
func TestCollectInterfaceMacs(t *testing.T) {
	t.Run("primary only when listMacs is nil", func(t *testing.T) {
		got := collectInterfaceMacs("AA:BB:CC:DD:EE:FF", nil)
		assert.Equal(t, []string{"aa:bb:cc:dd:ee:ff"}, got)
	})

	t.Run("dedupes against primary", func(t *testing.T) {
		got := collectInterfaceMacs("aa:bb:cc:dd:ee:ff", func() []string {
			return []string{"AA:BB:CC:DD:EE:FF", "11:11:11:11:11:11"}
		})
		assert.Equal(t, []string{"11:11:11:11:11:11", "aa:bb:cc:dd:ee:ff"}, got)
	})

	t.Run("skips empty MACs", func(t *testing.T) {
		got := collectInterfaceMacs("", func() []string {
			return []string{"", "aa:aa:aa:aa:aa:aa", ""}
		})
		assert.Equal(t, []string{"aa:aa:aa:aa:aa:aa"}, got)
	})

	t.Run("returns nil when nothing to publish", func(t *testing.T) {
		assert.Nil(t, collectInterfaceMacs("", nil))
	})
}

// TestMeshNeighborsWorker_OriginatorFailureStillPublishesNeighbors asserts
// the publish continues with an empty originator slice when `batctl oj`
// fails — neighbor data is useful on its own.
func TestMeshNeighborsWorker_OriginatorFailureStillPublishesNeighbors(t *testing.T) {
	neighbors := batmanadv.Neighbors{
		{HardIfname: "wlh0", NeighAddress: "aa:aa:aa:aa:aa:01", LastSeenMsecs: 100, Throughput: 30000},
	}

	fake := &fakeAlfredClient{}

	worker := &MeshNeighborsWorker{
		Config: &ManagementConfig{Log: zerolog.Nop(), BatInterface: "bat0"},
		Now:    func() time.Time { return time.Unix(0, 0) },
	}

	err := worker.sendOnceWithDeps(
		fake,
		func() (*batmanadv.Neighbors, error) { return &neighbors, nil },
		func() ([]batmanadv.Originator, error) { return nil, assert.AnError },
		func() (*batmanadv.MeshConfig, error) { return &batmanadv.MeshConfig{AlgoName: "BATMAN_IV"}, nil },
		func(string) network.NetworkInterface { return network.NetworkInterface{MAC: "aa:bb:cc:dd:ee:ff"} },
		func() (string, error) { return "node", nil },
		nil,
	)
	require.NoError(t, err, "orig failure must not abort the publish")

	got := &netv1.MeshNeighbors{}
	require.NoError(t, got.UnmarshalVT(fake.lastData))
	assert.Len(t, got.GetNeighbors(), 1)
	assert.Empty(t, got.GetOriginators(), "originators empty when batctl oj fails")
	assert.Equal(t, int32(4), got.GetAlgorithm(), "BATMAN_IV → 4")
}

// TestMeshNeighborsWorker_NeighborFailureAborts asserts a batctl nj
// failure is fatal for the tick (nothing useful to say without it).
func TestMeshNeighborsWorker_NeighborFailureAborts(t *testing.T) {
	fake := &fakeAlfredClient{}

	worker := &MeshNeighborsWorker{
		Config: &ManagementConfig{Log: zerolog.Nop(), BatInterface: "bat0"},
	}

	err := worker.sendOnceWithDeps(
		fake,
		func() (*batmanadv.Neighbors, error) { return nil, assert.AnError },
		func() ([]batmanadv.Originator, error) { return nil, nil },
		func() (*batmanadv.MeshConfig, error) { return &batmanadv.MeshConfig{}, nil },
		func(string) network.NetworkInterface { return network.NetworkInterface{} },
		func() (string, error) { return "", nil },
		nil,
	)
	require.Error(t, err)
	assert.Zero(t, fake.setCalls, "no publish happens when neighbor fetch fails")
}

// TestMeshNeighborsWorker_UnknownAlgorithmFallsBackToZero covers the
// case where `batctl mj` returns a value we don't recognize. The
// publish still ships; consumers treat algorithm=0 as "unknown".
func TestMeshNeighborsWorker_UnknownAlgorithmFallsBackToZero(t *testing.T) {
	fake := &fakeAlfredClient{}

	worker := &MeshNeighborsWorker{
		Config: &ManagementConfig{Log: zerolog.Nop(), BatInterface: "bat0"},
		Now:    func() time.Time { return time.Unix(0, 0) },
	}

	err := worker.sendOnceWithDeps(
		fake,
		func() (*batmanadv.Neighbors, error) { return &batmanadv.Neighbors{}, nil },
		func() ([]batmanadv.Originator, error) { return nil, nil },
		func() (*batmanadv.MeshConfig, error) {
			return &batmanadv.MeshConfig{AlgoName: "BATMAN_UNKNOWN"}, nil
		},
		func(string) network.NetworkInterface { return network.NetworkInterface{MAC: "aa:bb:cc:dd:ee:ff"} },
		func() (string, error) { return "host", nil },
		nil,
	)
	require.NoError(t, err)

	got := &netv1.MeshNeighbors{}
	require.NoError(t, got.UnmarshalVT(fake.lastData))
	assert.Equal(t, int32(0), got.GetAlgorithm())
}
