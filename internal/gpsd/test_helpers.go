package gpsd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// mockGPSDServer simulates a GPSD server for testing
type mockGPSDServer struct {
	listener net.Listener
	started  chan struct{}
	address  string
	messages []string
}

func newMockGPSDServer(t *testing.T) *mockGPSDServer {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create mock GPSD server: %v", err)
	}

	return &mockGPSDServer{
		listener: listener,
		address:  listener.Addr().String(),
		started:  make(chan struct{}),
	}
}

func (m *mockGPSDServer) Start() {
	close(m.started)

	go func() {
		for {
			conn, err := m.listener.Accept()
			if err != nil {
				return
			}

			go m.handleConnection(conn)
		}
	}()
}

func (m *mockGPSDServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Read the watch command
	buf := make([]byte, 1024)

	_, err := conn.Read(buf)
	if err != nil {
		return
	}

	// Send messages to client
	for _, msg := range m.messages {
		_, err := conn.Write([]byte(msg + "\n"))
		if err != nil {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	// Keep connection open
	time.Sleep(100 * time.Millisecond)
}

func (m *mockGPSDServer) Stop() {
	m.listener.Close()
}

func (m *mockGPSDServer) AddTPVMessage(lat, lon, alt, speed, track float64, mode int) {
	tpv := TPVReport{
		Class:  "TPV",
		Mode:   mode,
		Time:   time.Now().UTC().Format(time.RFC3339),
		Lat:    lat,
		Lon:    lon,
		Alt:    alt,
		Speed:  speed,
		Track:  track,
		Climb:  0,
		Device: "/dev/ttyUSB0",
	}

	data, _ := json.Marshal(tpv)
	m.messages = append(m.messages, string(data))
}

func (m *mockGPSDServer) AddSKYMessage(hdop float64, uSat int) {
	sky := SKYReport{
		Class: "SKY",
		Time:  time.Now().UTC().Format(time.RFC3339),
		HDOP:  hdop,
		VDOP:  hdop * 1.5,
		PDOP:  hdop * 2.0,
		NSat:  uSat + 4,
		USat:  uSat,
	}

	data, _ := json.Marshal(sky)
	m.messages = append(m.messages, string(data))
}

// AddSKYMessageWithSatellites adds a SKY message with individual satellite entries
func (m *mockGPSDServer) AddSKYMessageWithSatellites(hdop float64, uSat int, satellites []SatelliteInfo) {
	sky := SKYReport{
		Class: "SKY",
		Time:  time.Now().UTC().Format(time.RFC3339),
		HDOP:  hdop,
		VDOP:  hdop * 1.5,
		PDOP:  hdop * 2.0,
		NSat:  len(satellites),
		USat:  uSat,
	}

	for _, s := range satellites {
		sky.Satellites = append(sky.Satellites, struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{PRN: s.PRN, El: s.El, Az: s.Az, Ss: s.Ss, Used: s.Used})
	}

	data, _ := json.Marshal(sky)
	m.messages = append(m.messages, string(data))
}

// Helper function to verify NMEA checksum
func verifyNMEAChecksum(nmea string) bool {
	if !strings.HasPrefix(nmea, "$") || !strings.Contains(nmea, "*") {
		return false
	}

	parts := strings.Split(nmea[1:], "*")
	if len(parts) != 2 {
		return false
	}

	sentence := parts[0]
	expectedChecksum := calculateNMEAChecksum(sentence)

	var actualChecksum byte

	_, _ = fmt.Sscanf(parts[1], "%02X", &actualChecksum)

	return expectedChecksum == actualChecksum
}
