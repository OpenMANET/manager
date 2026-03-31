package batmanadv

import (
	"encoding/binary"
	"testing"

	"github.com/mdlayher/netlink"
)

// makeUint32Attr creates a netlink attribute with a little-endian uint32 value.
func makeUint32Attr(typ uint16, val uint32) netlink.Attribute {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, val)

	return netlink.Attribute{Type: typ, Data: data}
}

// makeUint8Attr creates a netlink attribute with a single byte value.
func makeUint8Attr(typ uint16, val uint8) netlink.Attribute {
	return netlink.Attribute{Type: typ, Data: []byte{val}}
}

// makeStringAttr creates a netlink attribute with a null-terminated string.
func makeStringAttr(typ uint16, val string) netlink.Attribute {
	return netlink.Attribute{Type: typ, Data: append([]byte(val), 0)}
}

// makeMACAttr creates a netlink attribute with a 6-byte MAC address.
func makeMACAttr(typ uint16, mac [6]byte) netlink.Attribute {
	return netlink.Attribute{Type: typ, Data: mac[:]}
}

func TestParseMeshConfig_AllFields(t *testing.T) {
	attrs := []netlink.Attribute{
		makeStringAttr(BatadvAttrVersion, "2023.1"),
		makeStringAttr(BatadvAttrAlgoName, "BATMAN_IV"),
		makeUint32Attr(BatadvAttrMeshIfindex, 10),
		makeStringAttr(BatadvAttrMeshIfname, "bat0"),
		makeMACAttr(BatadvAttrMeshAddress, [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}),
		makeUint32Attr(BatadvAttrHardIfindex, 3),
		makeStringAttr(BatadvAttrHardIfname, "wlan0"),
		makeMACAttr(BatadvAttrHardAddress, [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}),
		makeUint8Attr(BatadvAttrGwMode, BatadvGwModeServer),
		makeUint32Attr(BatadvAttrGwBandwidthDown, 10000),
		makeUint32Attr(BatadvAttrGwBandwidthUp, 2000),
		makeUint32Attr(BatadvAttrGwSelClass, 20),
		makeUint32Attr(BatadvAttrHopPenalty, 15),
		makeUint32Attr(BatadvAttrOrigInterval, 1000),
		makeUint32Attr(BatadvAttrMulticastFanout, 16),
		makeUint32Attr(BatadvAttrTTTTVN, 42),
		makeUint32Attr(BatadvAttrBLACRC, 12345),
		makeUint32Attr(BatadvAttrIsolationMark, 10),
		makeUint32Attr(BatadvAttrIsolationMask, 20),
		makeUint8Attr(BatadvAttrAggregatedOgmsEnabled, 1),
		makeUint8Attr(BatadvAttrAPisolationEnabled, 0),
		makeUint8Attr(BatadvAttrBondingEnabled, 1),
		makeUint8Attr(BatadvAttrBridgeLoopAvoidEnabled, 1),
		makeUint8Attr(BatadvAttrDATEnabled, 1),
		makeUint8Attr(BatadvAttrFragEnabled, 1),
		makeUint8Attr(BatadvAttrMulticastForcefloodEnable, 0),
		// mcast_flags: want_all_ipv4 (bit 1) = 0x02
		makeUint32Attr(BatadvAttrMcastFlags, 0x02),
		// mcast_flags_priv: bridged (bit 0) + querier_ipv4_exists (bit 1) = 0x03
		makeUint32Attr(BatadvAttrMcastFlagsPriv, 0x03),
	}

	cfg, err := parseMeshConfig(attrs)
	if err != nil {
		t.Fatalf("parseMeshConfig() error = %v", err)
	}

	// String fields
	if cfg.Version != "2023.1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "2023.1")
	}

	if cfg.AlgoName != "BATMAN_IV" {
		t.Errorf("AlgoName = %q, want %q", cfg.AlgoName, "BATMAN_IV")
	}

	if cfg.MeshIfname != "bat0" {
		t.Errorf("MeshIfname = %q, want %q", cfg.MeshIfname, "bat0")
	}

	if cfg.MeshAddress != "02:00:00:00:00:01" {
		t.Errorf("MeshAddress = %q, want %q", cfg.MeshAddress, "02:00:00:00:00:01")
	}

	if cfg.HardIfname != "wlan0" {
		t.Errorf("HardIfname = %q, want %q", cfg.HardIfname, "wlan0")
	}

	if cfg.HardAddress != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("HardAddress = %q, want %q", cfg.HardAddress, "aa:bb:cc:dd:ee:ff")
	}

	if cfg.GwMode != "server" {
		t.Errorf("GwMode = %q, want %q", cfg.GwMode, "server")
	}

	// Integer fields
	if cfg.MeshIfindex != 10 {
		t.Errorf("MeshIfindex = %d, want %d", cfg.MeshIfindex, 10)
	}

	if cfg.HardIfindex != 3 {
		t.Errorf("HardIfindex = %d, want %d", cfg.HardIfindex, 3)
	}

	if cfg.GwBandwidthDown != 10000 {
		t.Errorf("GwBandwidthDown = %d, want %d", cfg.GwBandwidthDown, 10000)
	}

	if cfg.GwBandwidthUp != 2000 {
		t.Errorf("GwBandwidthUp = %d, want %d", cfg.GwBandwidthUp, 2000)
	}

	if cfg.GwSelClass != 20 {
		t.Errorf("GwSelClass = %d, want %d", cfg.GwSelClass, 20)
	}

	if cfg.HopPenalty != 15 {
		t.Errorf("HopPenalty = %d, want %d", cfg.HopPenalty, 15)
	}

	if cfg.OrigInterval != 1000 {
		t.Errorf("OrigInterval = %d, want %d", cfg.OrigInterval, 1000)
	}

	if cfg.MulticastFanout != 16 {
		t.Errorf("MulticastFanout = %d, want %d", cfg.MulticastFanout, 16)
	}

	if cfg.TtTtvn != 42 {
		t.Errorf("TtTtvn = %d, want %d", cfg.TtTtvn, 42)
	}

	if cfg.BlaCrc != 12345 {
		t.Errorf("BlaCrc = %d, want %d", cfg.BlaCrc, 12345)
	}

	if cfg.IsolationMark != 10 {
		t.Errorf("IsolationMark = %d, want %d", cfg.IsolationMark, 10)
	}

	if cfg.IsolationMask != 20 {
		t.Errorf("IsolationMask = %d, want %d", cfg.IsolationMask, 20)
	}

	// Boolean fields
	if !cfg.AggregatedOgmsEnabled {
		t.Error("AggregatedOgmsEnabled should be true")
	}

	if cfg.ApIsolationEnabled {
		t.Error("ApIsolationEnabled should be false")
	}

	if !cfg.BondingEnabled {
		t.Error("BondingEnabled should be true")
	}

	if !cfg.BridgeLoopAvoidanceEnabled {
		t.Error("BridgeLoopAvoidanceEnabled should be true")
	}

	if !cfg.DistributedArpTableEnabled {
		t.Error("DistributedArpTableEnabled should be true")
	}

	if !cfg.FragmentationEnabled {
		t.Error("FragmentationEnabled should be true")
	}

	if cfg.MulticastForcefloodEnabled {
		t.Error("MulticastForcefloodEnabled should be false")
	}
}

func TestParseMeshConfig_McastFlagsBitmask(t *testing.T) {
	tests := []struct {
		name           string
		raw            uint32
		wantAllUnsnoop bool
		wantAllIPv4    bool
		wantAllIPv6    bool
		wantNoRtrIPv4  bool
		wantNoRtrIPv6  bool
	}{
		{
			name:           "all flags set",
			raw:            0x1F, // bits 0-4
			wantAllUnsnoop: true, wantAllIPv4: true, wantAllIPv6: true,
			wantNoRtrIPv4: true, wantNoRtrIPv6: true,
		},
		{
			name:           "no flags set",
			raw:            0x00,
			wantAllUnsnoop: false, wantAllIPv4: false, wantAllIPv6: false,
			wantNoRtrIPv4: false, wantNoRtrIPv6: false,
		},
		{
			name:           "only want_all_ipv4",
			raw:            0x02,
			wantAllUnsnoop: false, wantAllIPv4: true, wantAllIPv6: false,
			wantNoRtrIPv4: false, wantNoRtrIPv6: false,
		},
		{
			name:           "ipv4 and ipv6",
			raw:            0x06, // bits 1,2
			wantAllUnsnoop: false, wantAllIPv4: true, wantAllIPv6: true,
			wantNoRtrIPv4: false, wantNoRtrIPv6: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := decodeMcastFlags(tt.raw)

			if flags.AllUnsnoopables != tt.wantAllUnsnoop {
				t.Errorf("AllUnsnoopables = %v, want %v", flags.AllUnsnoopables, tt.wantAllUnsnoop)
			}

			if flags.WantAllIpv4 != tt.wantAllIPv4 {
				t.Errorf("WantAllIpv4 = %v, want %v", flags.WantAllIpv4, tt.wantAllIPv4)
			}

			if flags.WantAllIpv6 != tt.wantAllIPv6 {
				t.Errorf("WantAllIpv6 = %v, want %v", flags.WantAllIpv6, tt.wantAllIPv6)
			}

			if flags.WantNoRtrIpv4 != tt.wantNoRtrIPv4 {
				t.Errorf("WantNoRtrIpv4 = %v, want %v", flags.WantNoRtrIpv4, tt.wantNoRtrIPv4)
			}

			if flags.WantNoRtrIpv6 != tt.wantNoRtrIPv6 {
				t.Errorf("WantNoRtrIpv6 = %v, want %v", flags.WantNoRtrIpv6, tt.wantNoRtrIPv6)
			}

			if flags.Raw != int(tt.raw) {
				t.Errorf("Raw = %d, want %d", flags.Raw, tt.raw)
			}
		})
	}
}

func TestParseMeshConfig_McastFlagsPrivBitmask(t *testing.T) {
	tests := []struct {
		name              string
		raw               uint32
		wantBridged       bool
		wantQuerierIPv4   bool
		wantQuerierIPv6   bool
		wantShadowingIPv4 bool
		wantShadowingIPv6 bool
	}{
		{
			name:        "all flags set",
			raw:         0x1F,
			wantBridged: true, wantQuerierIPv4: true, wantQuerierIPv6: true,
			wantShadowingIPv4: true, wantShadowingIPv6: true,
		},
		{
			name:        "bridged + querier ipv4",
			raw:         0x03,
			wantBridged: true, wantQuerierIPv4: true, wantQuerierIPv6: false,
			wantShadowingIPv4: false, wantShadowingIPv6: false,
		},
		{
			name:        "none set",
			raw:         0x00,
			wantBridged: false, wantQuerierIPv4: false, wantQuerierIPv6: false,
			wantShadowingIPv4: false, wantShadowingIPv6: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := decodeMcastFlagsPriv(tt.raw)

			if flags.Bridged != tt.wantBridged {
				t.Errorf("Bridged = %v, want %v", flags.Bridged, tt.wantBridged)
			}

			if flags.QuerierIpv4Exists != tt.wantQuerierIPv4 {
				t.Errorf("QuerierIpv4Exists = %v, want %v", flags.QuerierIpv4Exists, tt.wantQuerierIPv4)
			}

			if flags.QuerierIpv6Exists != tt.wantQuerierIPv6 {
				t.Errorf("QuerierIpv6Exists = %v, want %v", flags.QuerierIpv6Exists, tt.wantQuerierIPv6)
			}

			if flags.QuerierIpv4Shadowing != tt.wantShadowingIPv4 {
				t.Errorf("QuerierIpv4Shadowing = %v, want %v", flags.QuerierIpv4Shadowing, tt.wantShadowingIPv4)
			}

			if flags.QuerierIpv6Shadowing != tt.wantShadowingIPv6 {
				t.Errorf("QuerierIpv6Shadowing = %v, want %v", flags.QuerierIpv6Shadowing, tt.wantShadowingIPv6)
			}

			if flags.Raw != int(tt.raw) {
				t.Errorf("Raw = %d, want %d", flags.Raw, tt.raw)
			}
		})
	}
}

func TestParseMeshConfig_GatewayModes(t *testing.T) {
	tests := []struct {
		name    string
		mode    uint8
		wantStr string
	}{
		{"off", BatadvGwModeOff, "off"},
		{"client", BatadvGwModeClient, "client"},
		{"server", BatadvGwModeServer, "server"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := []netlink.Attribute{
				makeUint8Attr(BatadvAttrGwMode, tt.mode),
			}

			cfg, err := parseMeshConfig(attrs)
			if err != nil {
				t.Fatalf("parseMeshConfig() error = %v", err)
			}

			if cfg.GwMode != tt.wantStr {
				t.Errorf("GwMode = %q, want %q", cfg.GwMode, tt.wantStr)
			}
		})
	}
}

func TestParseMeshConfig_MACFormatting(t *testing.T) {
	attrs := []netlink.Attribute{
		makeMACAttr(BatadvAttrHardAddress, [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}),
		makeMACAttr(BatadvAttrMeshAddress, [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}),
	}

	cfg, err := parseMeshConfig(attrs)
	if err != nil {
		t.Fatalf("parseMeshConfig() error = %v", err)
	}

	if cfg.HardAddress != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("HardAddress = %q, want %q", cfg.HardAddress, "aa:bb:cc:dd:ee:ff")
	}

	if cfg.MeshAddress != "00:11:22:33:44:55" {
		t.Errorf("MeshAddress = %q, want %q", cfg.MeshAddress, "00:11:22:33:44:55")
	}
}

func TestParseMeshConfig_NullTerminatedStrings(t *testing.T) {
	attrs := []netlink.Attribute{
		{Type: BatadvAttrMeshIfname, Data: []byte("bat0\x00")},
		{Type: BatadvAttrHardIfname, Data: []byte("wlan0\x00\x00\x00")},
		{Type: BatadvAttrVersion, Data: []byte("2023.1\x00")},
	}

	cfg, err := parseMeshConfig(attrs)
	if err != nil {
		t.Fatalf("parseMeshConfig() error = %v", err)
	}

	if cfg.MeshIfname != "bat0" {
		t.Errorf("MeshIfname = %q, want %q", cfg.MeshIfname, "bat0")
	}

	if cfg.HardIfname != "wlan0" {
		t.Errorf("HardIfname = %q, want %q", cfg.HardIfname, "wlan0")
	}

	if cfg.Version != "2023.1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "2023.1")
	}
}

func TestParseMeshConfig_EmptyAttrs(t *testing.T) {
	cfg, err := parseMeshConfig(nil)
	if err != nil {
		t.Fatalf("parseMeshConfig(nil) error = %v", err)
	}

	// Should return zero-value struct, no error
	if cfg.Version != "" {
		t.Errorf("Version = %q, want empty", cfg.Version)
	}

	if cfg.MeshIfindex != 0 {
		t.Errorf("MeshIfindex = %d, want 0", cfg.MeshIfindex)
	}

	if cfg.GwMode != "" {
		t.Errorf("GwMode = %q, want empty", cfg.GwMode)
	}
}

func TestParseMeshConfig_MissingOptionalAttrs(t *testing.T) {
	// Only provide required-ish attrs, omit optional ones
	attrs := []netlink.Attribute{
		makeUint32Attr(BatadvAttrMeshIfindex, 10),
		makeStringAttr(BatadvAttrMeshIfname, "bat0"),
	}

	cfg, err := parseMeshConfig(attrs)
	if err != nil {
		t.Fatalf("parseMeshConfig() error = %v", err)
	}

	if cfg.MeshIfindex != 10 {
		t.Errorf("MeshIfindex = %d, want 10", cfg.MeshIfindex)
	}

	// Omitted fields should be zero-values
	if cfg.HardAddress != "" {
		t.Errorf("HardAddress = %q, want empty", cfg.HardAddress)
	}

	if cfg.GwBandwidthDown != 0 {
		t.Errorf("GwBandwidthDown = %d, want 0", cfg.GwBandwidthDown)
	}

	if cfg.AggregatedOgmsEnabled {
		t.Error("AggregatedOgmsEnabled should be false when not present")
	}
}

func TestParseGateway_AllFields(t *testing.T) {
	attrs := []netlink.Attribute{
		makeUint32Attr(BatadvAttrHardIfindex, 3),
		makeStringAttr(BatadvAttrHardIfname, "wlan0"),
		makeMACAttr(BatadvAttrOrigAddress, [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}),
		makeMACAttr(BatadvAttrRouter, [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}),
		makeUint32Attr(BatadvAttrThroughput, 10000),
		makeUint32Attr(BatadvAttrBandwidthUp, 2000),
		makeUint32Attr(BatadvAttrBandwidthDown, 10000),
		makeUint8Attr(BatadvAttrFlagBest, 1),
	}

	gw, err := parseGateway(attrs)
	if err != nil {
		t.Fatalf("parseGateway() error = %v", err)
	}

	if gw.HardIfindex != 3 {
		t.Errorf("HardIfindex = %d, want 3", gw.HardIfindex)
	}

	if gw.HardIfname != "wlan0" {
		t.Errorf("HardIfname = %q, want %q", gw.HardIfname, "wlan0")
	}

	if gw.OrigAddress != "aa:bb:cc:dd:ee:01" {
		t.Errorf("OrigAddress = %q, want %q", gw.OrigAddress, "aa:bb:cc:dd:ee:01")
	}

	if gw.Router != "aa:bb:cc:dd:ee:01" {
		t.Errorf("Router = %q, want %q", gw.Router, "aa:bb:cc:dd:ee:01")
	}

	if gw.Throughput != 10000 {
		t.Errorf("Throughput = %d, want 10000", gw.Throughput)
	}

	if gw.BandwidthUp != 2000 {
		t.Errorf("BandwidthUp = %d, want 2000", gw.BandwidthUp)
	}

	if gw.BandwidthDown != 10000 {
		t.Errorf("BandwidthDown = %d, want 10000", gw.BandwidthDown)
	}

	if !gw.Best {
		t.Error("Best should be true")
	}
}

func TestParseGateway_NotBest(t *testing.T) {
	attrs := []netlink.Attribute{
		makeMACAttr(BatadvAttrOrigAddress, [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x02}),
		makeUint8Attr(BatadvAttrFlagBest, 0),
	}

	gw, err := parseGateway(attrs)
	if err != nil {
		t.Fatalf("parseGateway() error = %v", err)
	}

	if gw.Best {
		t.Error("Best should be false")
	}

	if gw.OrigAddress != "aa:bb:cc:dd:ee:02" {
		t.Errorf("OrigAddress = %q, want %q", gw.OrigAddress, "aa:bb:cc:dd:ee:02")
	}
}

func TestParseNeighbor_AllFields(t *testing.T) {
	attrs := []netlink.Attribute{
		makeUint32Attr(BatadvAttrHardIfindex, 3),
		makeStringAttr(BatadvAttrHardIfname, "wlan0"),
		makeMACAttr(BatadvAttrNeighAddress, [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}),
		makeUint32Attr(BatadvAttrLastSeenMsecs, 500),
		makeUint32Attr(BatadvAttrThroughput, 8000),
	}

	n, err := parseNeighbor(attrs)
	if err != nil {
		t.Fatalf("parseNeighbor() error = %v", err)
	}

	if n.HardIfindex != 3 {
		t.Errorf("HardIfindex = %d, want 3", n.HardIfindex)
	}

	if n.HardIfname != "wlan0" {
		t.Errorf("HardIfname = %q, want %q", n.HardIfname, "wlan0")
	}

	if n.NeighAddress != "11:22:33:44:55:66" {
		t.Errorf("NeighAddress = %q, want %q", n.NeighAddress, "11:22:33:44:55:66")
	}

	if n.LastSeenMsecs != 500 {
		t.Errorf("LastSeenMsecs = %d, want 500", n.LastSeenMsecs)
	}

	if n.Throughput != 8000 {
		t.Errorf("Throughput = %d, want 8000", n.Throughput)
	}
}

func TestParseOriginator_AllFields(t *testing.T) {
	attrs := []netlink.Attribute{
		makeMACAttr(BatadvAttrOrigAddress, [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}),
		makeStringAttr(BatadvAttrHardIfname, "wlan0"),
		makeMACAttr(BatadvAttrNeighAddress, [6]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x02}),
		makeUint32Attr(BatadvAttrLastSeenMsecs, 1234),
		makeUint8Attr(BatadvAttrTQ, 200),
	}

	o, err := parseOriginator(attrs)
	if err != nil {
		t.Fatalf("parseOriginator() error = %v", err)
	}

	if o.OrigAddress != "aa:bb:cc:dd:ee:01" {
		t.Errorf("OrigAddress = %q, want %q", o.OrigAddress, "aa:bb:cc:dd:ee:01")
	}

	if o.HardIfname != "wlan0" {
		t.Errorf("HardIfname = %q, want %q", o.HardIfname, "wlan0")
	}

	if o.BestNeigh != "aa:bb:cc:dd:ee:02" {
		t.Errorf("BestNeigh = %q, want %q", o.BestNeigh, "aa:bb:cc:dd:ee:02")
	}

	if o.LastSeenMs != 1234 {
		t.Errorf("LastSeenMs = %d, want 1234", o.LastSeenMs)
	}

	if o.TQ != 200 {
		t.Errorf("TQ = %d, want 200", o.TQ)
	}
}

func TestParseOriginator_TQRange(t *testing.T) {
	tests := []struct {
		name string
		tq   uint8
		want int
	}{
		{"zero", 0, 0},
		{"mid", 128, 128},
		{"max", 255, 255},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := []netlink.Attribute{
				makeUint8Attr(BatadvAttrTQ, tt.tq),
			}

			o, err := parseOriginator(attrs)
			if err != nil {
				t.Fatalf("parseOriginator() error = %v", err)
			}

			if o.TQ != tt.want {
				t.Errorf("TQ = %d, want %d", o.TQ, tt.want)
			}
		})
	}
}

func TestFormatMAC(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "normal MAC",
			data: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
			want: "aa:bb:cc:dd:ee:ff",
		},
		{
			name: "all zeros",
			data: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			want: "00:00:00:00:00:00",
		},
		{
			name: "all ones",
			data: []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			want: "ff:ff:ff:ff:ff:ff",
		},
		{
			name: "too short",
			data: []byte{0xaa, 0xbb},
			want: "",
		},
		{
			name: "nil",
			data: nil,
			want: "",
		},
		{
			name: "extra bytes ignored",
			data: []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22},
			want: "aa:bb:cc:dd:ee:ff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMAC(tt.data)
			if got != tt.want {
				t.Errorf("formatMAC() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrimNull(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no null", "wlan0", "wlan0"},
		{"single null", "wlan0\x00", "wlan0"},
		{"multiple nulls", "bat0\x00\x00\x00", "bat0"},
		{"empty", "", ""},
		{"only nulls", "\x00\x00", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimNull(tt.input)
			if got != tt.want {
				t.Errorf("trimNull(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNlUint32(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint32
	}{
		{"normal", []byte{0x10, 0x27, 0x00, 0x00}, 10000},
		{"max", []byte{0xFF, 0xFF, 0xFF, 0xFF}, 0xFFFFFFFF},
		{"zero", []byte{0x00, 0x00, 0x00, 0x00}, 0},
		{"too short", []byte{0x01, 0x02}, 0},
		{"nil", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nlUint32(tt.data)
			if got != tt.want {
				t.Errorf("nlUint32() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNlBool(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"true (1)", []byte{1}, true},
		{"true (nonzero)", []byte{42}, true},
		{"false (0)", []byte{0}, false},
		{"empty", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nlBool(tt.data)
			if got != tt.want {
				t.Errorf("nlBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseMeshConfig_UnknownGwMode(t *testing.T) {
	attrs := []netlink.Attribute{
		makeUint8Attr(BatadvAttrGwMode, 99),
	}

	cfg, err := parseMeshConfig(attrs)
	if err != nil {
		t.Fatalf("parseMeshConfig() error = %v", err)
	}

	if cfg.GwMode != "unknown(99)" {
		t.Errorf("GwMode = %q, want %q", cfg.GwMode, "unknown(99)")
	}
}
