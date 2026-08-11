package gpsd

import (
	"net"
	"testing"
	"time"

	"github.com/coreywagehoft/go-tak/pkg/cot"
	"github.com/coreywagehoft/go-tak/pkg/cotproto"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
)

// Tests for cot_listener.go functions:
// - parseCoTPosition / parseCoTPositionProto / parseCoTPositionXML
// - isKnownEUD / getDHCPLeases
// - applyExternalPosition
// - handleIncomingCoT
// - isTimeout

func makeProtoMeshDatagram(t *testing.T, uid string, lat, lon, hae, speed, course float64) []byte {
	t.Helper()

	msg := &cotproto.TakMessage{
		CotEvent: &cotproto.CotEvent{
			Uid: uid,
			Lat: lat,
			Lon: lon,
			Hae: hae,
			Detail: &cotproto.Detail{
				Track: &cotproto.Track{
					Speed:  speed,
					Course: course,
				},
			},
		},
	}

	data, err := cot.MakeProtoMeshPacketV1(msg)
	if err != nil {
		t.Fatalf("failed to build test mesh packet: %v", err)
	}

	return data
}

func TestParseCoTPosition_Proto(t *testing.T) {
	data := makeProtoMeshDatagram(t, "ANDROID-test123", 37.7749, -122.4194, 45.5, 1.5, 90.0)

	pos, ok := parseCoTPosition(data)
	if !ok {
		t.Fatal("expected ok=true for valid mesh protobuf datagram")
	}

	if pos.uid != "ANDROID-test123" {
		t.Errorf("uid = %q, want %q", pos.uid, "ANDROID-test123")
	}

	if pos.lat != 37.7749 {
		t.Errorf("lat = %v, want %v", pos.lat, 37.7749)
	}

	if pos.lon != -122.4194 {
		t.Errorf("lon = %v, want %v", pos.lon, -122.4194)
	}

	if pos.hae != 45.5 {
		t.Errorf("hae = %v, want %v", pos.hae, 45.5)
	}

	if pos.speed != 1.5 {
		t.Errorf("speed = %v, want %v", pos.speed, 1.5)
	}

	if pos.course != 90.0 {
		t.Errorf("course = %v, want %v", pos.course, 90.0)
	}
}

func TestParseCoTPosition_ProtoZeroLatLonRejected(t *testing.T) {
	data := makeProtoMeshDatagram(t, "ANDROID-test123", 0, 0, 0, 0, 0)

	_, ok := parseCoTPosition(data)
	if ok {
		t.Error("expected ok=false when lat and lon are both zero")
	}
}

func TestParseCoTPosition_ProtoGarbagePayloadRejected(t *testing.T) {
	// Valid mesh framing (magic, version, magic) but garbage protobuf payload.
	data := []byte{0xbf, 0x01, 0xbf, 0xff, 0xff, 0xff}

	_, ok := parseCoTPosition(data)
	if ok {
		t.Error("expected ok=false for garbage protobuf payload")
	}
}

func TestParseCoTPosition_XML(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0"?>
<event version="2.0" uid="ANDROID-xml456" type="a-f-G-E-S" how="m-g" time="2026-07-09T00:00:00Z" start="2026-07-09T00:00:00Z" stale="2026-07-09T00:05:00Z">
  <point lat="-32.9389" lon="-71.527307" hae="114.7" ce="9999999" le="9999999"/>
</event>`)

	pos, ok := parseCoTPosition(xmlData)
	if !ok {
		t.Fatal("expected ok=true for valid XML CoT event")
	}

	if pos.uid != "ANDROID-xml456" {
		t.Errorf("uid = %q, want %q", pos.uid, "ANDROID-xml456")
	}

	if pos.lat != -32.9389 {
		t.Errorf("lat = %v, want %v", pos.lat, -32.9389)
	}

	if pos.lon != -71.527307 {
		t.Errorf("lon = %v, want %v", pos.lon, -71.527307)
	}

	if pos.hae != 114.7 {
		t.Errorf("hae = %v, want %v", pos.hae, 114.7)
	}
}

func TestParseCoTPosition_XMLZeroLatLonRejected(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0"?>
<event version="2.0" uid="ANDROID-xml456" type="a-f-G-E-S" how="m-g" time="2026-07-09T00:00:00Z" start="2026-07-09T00:00:00Z" stale="2026-07-09T00:05:00Z">
  <point lat="0" lon="0" hae="0" ce="9999999" le="9999999"/>
</event>`)

	_, ok := parseCoTPosition(xmlData)
	if ok {
		t.Error("expected ok=false when lat and lon are both zero")
	}
}

func TestParseCoTPosition_MalformedXMLRejected(t *testing.T) {
	_, ok := parseCoTPosition([]byte("<not-even-xml"))
	if ok {
		t.Error("expected ok=false for malformed XML")
	}
}

func TestParseCoTPosition_EmptyDataRejected(t *testing.T) {
	_, ok := parseCoTPosition(nil)
	if ok {
		t.Error("expected ok=false for empty datagram")
	}

	_, ok = parseCoTPosition([]byte{})
	if ok {
		t.Error("expected ok=false for empty (non-nil) datagram")
	}
}

func TestIsKnownEUD(t *testing.T) {
	tests := []struct {
		name    string
		leases  *network.DHCPLeasesResponse
		err     error
		ipAddr  string
		wantEUD bool
	}{
		{
			name: "matching lease",
			leases: &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{
					{Hostname: "S25-Ultra-de-Felipe", IPAddr: "10.41.0.187"},
					{Hostname: "PC_Pipe", IPAddr: "10.41.0.180"},
				},
			},
			ipAddr:  "10.41.0.187",
			wantEUD: true,
		},
		{
			name: "no matching lease",
			leases: &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{
					{Hostname: "PC_Pipe", IPAddr: "10.41.0.180"},
				},
			},
			ipAddr:  "10.41.0.187",
			wantEUD: false,
		},
		{
			name:    "no leases at all",
			leases:  &network.DHCPLeasesResponse{},
			ipAddr:  "10.41.0.187",
			wantEUD: false,
		},
		{
			name:    "lease lookup error",
			leases:  nil,
			err:     errBoom,
			ipAddr:  "10.41.0.187",
			wantEUD: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gps := &GPSService{
				Log: zerolog.Nop(),
				GetDHCPLeases: func() (*network.DHCPLeasesResponse, error) {
					return tc.leases, tc.err
				},
			}

			got := gps.isKnownEUD(tc.ipAddr)
			if got != tc.wantEUD {
				t.Errorf("isKnownEUD(%q) = %v, want %v", tc.ipAddr, got, tc.wantEUD)
			}
		})
	}
}

// errBoom is a sentinel error for fake DHCP lease lookups.
var errBoom = &staticError{"boom"}

type staticError struct{ msg string }

func (e *staticError) Error() string { return e.msg }

func TestApplyExternalPosition(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	gps.applyExternalPosition(cotPosition{
		uid:    "ANDROID-test123",
		lat:    -32.9389,
		lon:    -71.527307,
		hae:    114.7,
		speed:  1.2,
		course: 275.0,
	})

	pos := gps.GetPosition()

	if !pos.Valid {
		t.Error("expected Valid=true after applying an external position")
	}

	if pos.Mode != 3 {
		t.Errorf("Mode = %d, want 3 (externally-sourced fixes are treated as 3D)", pos.Mode)
	}

	if pos.Latitude != -32.9389 {
		t.Errorf("Latitude = %v, want %v", pos.Latitude, -32.9389)
	}

	if pos.Longitude != -71.527307 {
		t.Errorf("Longitude = %v, want %v", pos.Longitude, -71.527307)
	}

	if pos.Altitude != 114.7 {
		t.Errorf("Altitude = %v, want %v", pos.Altitude, 114.7)
	}

	if pos.Speed != 1.2 {
		t.Errorf("Speed = %v, want %v", pos.Speed, 1.2)
	}

	if pos.Track != 275.0 {
		t.Errorf("Track = %v, want %v", pos.Track, 275.0)
	}

	if pos.Timestamp.IsZero() {
		t.Error("expected a non-zero Timestamp")
	}
}

func TestHandleIncomingCoT_KnownEUDAdoptsPosition(t *testing.T) {
	gps := &GPSService{
		Log: zerolog.Nop(),
		GetDHCPLeases: func() (*network.DHCPLeasesResponse, error) {
			return &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{
					{Hostname: "S25-Ultra-de-Felipe", IPAddr: "10.41.0.187"},
				},
			}, nil
		},
	}

	data := makeProtoMeshDatagram(t, "ANDROID-285d18211df5e02e", -32.9389, -71.527307, 114.7, 0, 275)

	gps.handleIncomingCoT(data, "10.41.0.187")

	pos := gps.GetPosition()
	if !pos.Valid {
		t.Fatal("expected position to be adopted from a known EUD")
	}

	if pos.Latitude != -32.9389 || pos.Longitude != -71.527307 {
		t.Errorf("position = (%v, %v), want (-32.9389, -71.527307)", pos.Latitude, pos.Longitude)
	}
}

func TestExternalCoTSelected(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		// A GPSService can be constructed without a Config; the listener
		// goroutine must read that as "internal" instead of panicking.
		{name: "nil config reads as internal", cfg: nil, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gps := &GPSService{Log: zerolog.Nop(), Config: tc.cfg}

			if got := gps.externalCoTSelected(); got != tc.want {
				t.Errorf("externalCoTSelected() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandleIncomingCoT_ReannounceIsSingleFlight(t *testing.T) {
	gps := &GPSService{
		Log: zerolog.Nop(),
		GetDHCPLeases: func() (*network.DHCPLeasesResponse, error) {
			return &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{
					{Hostname: "S25-Ultra-de-Felipe", IPAddr: "10.41.0.187"},
				},
			}, nil
		},
	}

	// Stand in for a re-announce that is already running.
	gps.reannouncing.Store(true)

	data := makeProtoMeshDatagram(t, "ANDROID-285d18211df5e02e", -32.9389, -71.527307, 114.7, 0, 275)

	gps.handleIncomingCoT(data, "10.41.0.187")

	pos := gps.GetPosition()
	if !pos.Valid {
		t.Fatal("position should still be adopted while a re-announce is in flight")
	}

	// A second re-announce would clear the guard when it finished, and it
	// cannot acquire the guard while it is already held — so still being
	// set proves no overlapping goroutine was started.
	if !gps.reannouncing.Load() {
		t.Error("expected the in-flight guard to still be held; an overlapping re-announce was started")
	}
}

func TestHandleIncomingCoT_UnknownSenderIgnored(t *testing.T) {
	gps := &GPSService{
		Log: zerolog.Nop(),
		GetDHCPLeases: func() (*network.DHCPLeasesResponse, error) {
			// Sender is not in the lease table — e.g. a relayed CoT from
			// elsewhere on the mesh, not a directly-connected EUD.
			return &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{
					{Hostname: "SomeoneElse", IPAddr: "10.41.5.99"},
				},
			}, nil
		},
	}

	data := makeProtoMeshDatagram(t, "ANDROID-not-mine", 1.0, 2.0, 3.0, 0, 0)

	gps.handleIncomingCoT(data, "10.41.0.187")

	pos := gps.GetPosition()
	if pos.Valid {
		t.Error("expected position to remain unset when the CoT sender is not a known EUD")
	}
}

func TestHandleIncomingCoT_UnparseableDatagramIgnored(t *testing.T) {
	gps := &GPSService{
		Log: zerolog.Nop(),
		GetDHCPLeases: func() (*network.DHCPLeasesResponse, error) {
			return &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{{IPAddr: "10.41.0.187"}},
			}, nil
		},
	}

	gps.handleIncomingCoT([]byte("not a cot event"), "10.41.0.187")

	pos := gps.GetPosition()
	if pos.Valid {
		t.Error("expected position to remain unset for an unparseable datagram")
	}
}

func TestIsTimeout(t *testing.T) {
	// A real net.Error with Timeout()==true, produced by an actual expired
	// deadline, is the most faithful fixture for this check.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatalf("failed to open udp socket for timeout fixture: %v", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("failed to set expired read deadline: %v", err)
	}

	buf := make([]byte, 16)
	_, _, readErr := conn.ReadFromUDP(buf)

	if !isTimeout(readErr) {
		t.Errorf("isTimeout(%v) = false, want true for an expired deadline", readErr)
	}

	if isTimeout(errBoom) {
		t.Error("isTimeout(non-net error) = true, want false")
	}
}
