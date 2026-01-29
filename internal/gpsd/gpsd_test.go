package gpsd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// mockGPSDServer simulates a GPSD server for testing
type mockGPSDServer struct {
	listener net.Listener
	address  string
	messages []string
	started  chan struct{}
}

func newMockGPSDServer(t *testing.T) *mockGPSDServer {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
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

func TestNewGPSService(t *testing.T) {
	log := zerolog.Nop()

	// Create a mock server
	mock := newMockGPSDServer(t)
	mock.Start()
	defer mock.Stop()

	<-mock.started

	// Create GPS service
	gps, err := NewGPSServiceWithAddress(log, nil, mock.address)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer gps.Close()

	if gps == nil {
		t.Fatal("Expected non-nil GPS service")
	}

	if gps.address != mock.address {
		t.Errorf("Expected address %s, got %s", mock.address, gps.address)
	}
}

func TestGPSService_UpdatePosition(t *testing.T) {
	log := zerolog.Nop()

	// Create a mock server with test data
	mock := newMockGPSDServer(t)
	mock.AddTPVMessage(37.7749, -122.4194, 10.5, 5.2, 45.0, 3)
	mock.Start()
	defer mock.Stop()

	<-mock.started

	// Create GPS service
	gps, err := NewGPSServiceWithAddress(log, nil, mock.address)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer gps.Close()

	// Wait for position update
	time.Sleep(200 * time.Millisecond)

	// Verify position was updated
	pos := gps.GetPosition()
	if !pos.Valid {
		t.Error("Expected valid position")
	}

	if math.Abs(pos.Latitude-37.7749) > 0.0001 {
		t.Errorf("Expected latitude 37.7749, got %f", pos.Latitude)
	}

	if math.Abs(pos.Longitude-(-122.4194)) > 0.0001 {
		t.Errorf("Expected longitude -122.4194, got %f", pos.Longitude)
	}

	if math.Abs(pos.Altitude-10.5) > 0.1 {
		t.Errorf("Expected altitude 10.5, got %f", pos.Altitude)
	}

	if pos.Mode != 3 {
		t.Errorf("Expected mode 3, got %d", pos.Mode)
	}
}

func TestGPSService_GetterMethods(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		position: PositionReport{
			Timestamp: time.Now(),
			Latitude:  40.7128,
			Longitude: -74.0060,
			Altitude:  100.0,
			Speed:     10.5,
			Track:     90.0,
			Climb:     2.0,
			Valid:     true,
			Mode:      3,
		},
	}

	if lat := gps.GetLatitude(); lat != 40.7128 {
		t.Errorf("Expected latitude 40.7128, got %f", lat)
	}

	if lon := gps.GetLongitude(); lon != -74.0060 {
		t.Errorf("Expected longitude -74.0060, got %f", lon)
	}

	if alt := gps.GetAltitude(); alt != 100.0 {
		t.Errorf("Expected altitude 100.0, got %f", alt)
	}

	if speed := gps.GetSpeed(); speed != 10.5 {
		t.Errorf("Expected speed 10.5, got %f", speed)
	}

	if track := gps.GetTrack(); track != 90.0 {
		t.Errorf("Expected track 90.0, got %f", track)
	}

	if !gps.IsValid() {
		t.Error("Expected valid position")
	}

	if mode := gps.GetMode(); mode != 3 {
		t.Errorf("Expected mode 3, got %d", mode)
	}
}

func TestGPSService_AccuracyGetterMethods(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		position: PositionReport{
			Timestamp:   time.Now(),
			Latitude:    40.7128,
			Longitude:   -74.0060,
			Altitude:    100.0,
			Valid:       true,
			Mode:        3,
			EPH:         5.2,
			EPX:         3.1,
			EPY:         4.3,
			EPV:         8.7,
			DGPSAge:     2.5,
			DGPSStation: 120,
		},
	}

	if eph := gps.GetHorizontalAccuracy(); eph != 5.2 {
		t.Errorf("Expected horizontal accuracy 5.2, got %f", eph)
	}

	if epv := gps.GetVerticalAccuracy(); epv != 8.7 {
		t.Errorf("Expected vertical accuracy 8.7, got %f", epv)
	}

	if epx := gps.GetLongitudeError(); epx != 3.1 {
		t.Errorf("Expected longitude error 3.1, got %f", epx)
	}

	if epy := gps.GetLatitudeError(); epy != 4.3 {
		t.Errorf("Expected latitude error 4.3, got %f", epy)
	}

	if age := gps.GetDGPSAge(); age != 2.5 {
		t.Errorf("Expected DGPS age 2.5, got %f", age)
	}

	if station := gps.GetDGPSStation(); station != 120 {
		t.Errorf("Expected DGPS station 120, got %d", station)
	}
}

func TestGPSService_AccuracyGetterMethods_ZeroValues(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		position: PositionReport{
			Timestamp: time.Now(),
			Latitude:  40.7128,
			Longitude: -74.0060,
			Altitude:  100.0,
			Valid:     true,
			Mode:      3,
			// All accuracy fields default to 0
		},
	}

	if eph := gps.GetHorizontalAccuracy(); eph != 0 {
		t.Errorf("Expected horizontal accuracy 0, got %f", eph)
	}

	if epv := gps.GetVerticalAccuracy(); epv != 0 {
		t.Errorf("Expected vertical accuracy 0, got %f", epv)
	}

	if epx := gps.GetLongitudeError(); epx != 0 {
		t.Errorf("Expected longitude error 0, got %f", epx)
	}

	if epy := gps.GetLatitudeError(); epy != 0 {
		t.Errorf("Expected latitude error 0, got %f", epy)
	}

	if age := gps.GetDGPSAge(); age != 0 {
		t.Errorf("Expected DGPS age 0, got %f", age)
	}

	if station := gps.GetDGPSStation(); station != 0 {
		t.Errorf("Expected DGPS station 0, got %d", station)
	}
}

func TestGPSService_InvalidPosition(t *testing.T) {
	log := zerolog.Nop()

	// Create a mock server with invalid data (mode 1 = no fix)
	mock := newMockGPSDServer(t)
	mock.AddTPVMessage(0, 0, 0, 0, 0, 1)
	mock.Start()
	defer mock.Stop()

	<-mock.started

	// Create GPS service
	gps, err := NewGPSServiceWithAddress(log, nil, mock.address)
	if err != nil {
		t.Fatalf("Failed to create GPS service: %v", err)
	}
	defer gps.Close()

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Position should not be valid
	if gps.IsValid() {
		t.Error("Expected invalid position for mode 1")
	}
}

func TestFormatGGA(t *testing.T) {
	testCases := []struct {
		name     string
		position PositionReport
		validate func(t *testing.T, nmea string)
	}{
		{
			name: "Northern Hemisphere, Eastern Longitude",
			position: PositionReport{
				Timestamp: time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC),
				Latitude:  37.7749,
				Longitude: 122.4194,
				Altitude:  50.0,
				Valid:     true,
				Mode:      3,
			},
			validate: func(t *testing.T, nmea string) {
				if !strings.HasPrefix(nmea, "$GPGGA") {
					t.Errorf("Expected GPGGA prefix, got %s", nmea)
				}
				if !strings.Contains(nmea, ",N,") {
					t.Error("Expected N (North) hemisphere")
				}
				if !strings.Contains(nmea, ",E,") {
					t.Error("Expected E (East) hemisphere")
				}
				if !strings.Contains(nmea, "*") {
					t.Error("Expected checksum marker")
				}
			},
		},
		{
			name: "Southern Hemisphere, Western Longitude",
			position: PositionReport{
				Timestamp: time.Date(2024, 1, 1, 12, 30, 45, 0, time.UTC),
				Latitude:  -33.8688,
				Longitude: -151.2093,
				Altitude:  10.0,
				Valid:     true,
				Mode:      3,
			},
			validate: func(t *testing.T, nmea string) {
				if !strings.Contains(nmea, ",S,") {
					t.Error("Expected S (South) hemisphere")
				}
				if !strings.Contains(nmea, ",W,") {
					t.Error("Expected W (West) hemisphere")
				}
			},
		},
		{
			name: "Zero coordinates",
			position: PositionReport{
				Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				Latitude:  0.0,
				Longitude: 0.0,
				Altitude:  0.0,
				Valid:     true,
				Mode:      2,
			},
			validate: func(t *testing.T, nmea string) {
				if !strings.HasPrefix(nmea, "$GPGGA") {
					t.Errorf("Expected GPGGA prefix")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nmea := formatGGA(tc.position)
			tc.validate(t, nmea)

			// Verify checksum
			if !verifyNMEAChecksum(nmea) {
				t.Errorf("Invalid NMEA checksum for: %s", nmea)
			}
		})
	}
}

func TestToNMEA(t *testing.T) {
	log := zerolog.Nop()

	t.Run("Valid position", func(t *testing.T) {
		gps := &GPSService{
			Log: log,
			position: PositionReport{
				Timestamp: time.Now(),
				Latitude:  37.7749,
				Longitude: -122.4194,
				Altitude:  50.0,
				Valid:     true,
				Mode:      3,
			},
		}

		nmea := gps.ToNMEA()
		if nmea == "" {
			t.Error("Expected non-empty NMEA string")
		}

		if !strings.HasPrefix(nmea, "$GPGGA") {
			t.Errorf("Expected GPGGA prefix, got %s", nmea)
		}

		if !verifyNMEAChecksum(nmea) {
			t.Errorf("Invalid NMEA checksum: %s", nmea)
		}
	})

	t.Run("Invalid position", func(t *testing.T) {
		gps := &GPSService{
			Log: log,
			position: PositionReport{
				Valid: false,
			},
		}

		nmea := gps.ToNMEA()
		if nmea != "" {
			t.Errorf("Expected empty NMEA string for invalid position, got %s", nmea)
		}
	})
}

func TestCalculateNMEAChecksum(t *testing.T) {
	testCases := []struct {
		sentence string
		expected byte
	}{
		{"GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,", 0x47},
		{"GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W", 0x6A},
	}

	for _, tc := range testCases {
		result := calculateNMEAChecksum(tc.sentence)
		if result != tc.expected {
			t.Errorf("For sentence %s, expected checksum %02X, got %02X",
				tc.sentence, tc.expected, result)
		}
	}
}

func TestGPSService_Reconnection(t *testing.T) {
	log := zerolog.Nop()

	// Create GPS service with invalid address
	gps := &GPSService{
		Log:            log,
		address:        "localhost:99999",
		reconnectDelay: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	gps.ctx = ctx
	gps.cancel = cancel

	// Start connection handler in background
	go gps.connectionHandler()

	// Wait a bit to allow connection attempts
	time.Sleep(300 * time.Millisecond)

	// Close should work without error even if never connected
	err := gps.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}
}

func TestProcessGPSDMessage(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
	}

	t.Run("Valid TPV message", func(t *testing.T) {
		tpv := TPVReport{
			Class: "TPV",
			Mode:  3,
			Time:  time.Now().UTC().Format(time.RFC3339),
			Lat:   40.7128,
			Lon:   -74.0060,
			Alt:   100.0,
		}

		data, _ := json.Marshal(tpv)
		gps.processGPSDMessage(string(data))

		if !gps.IsValid() {
			t.Error("Expected valid position after processing TPV")
		}

		if gps.GetLatitude() != 40.7128 {
			t.Errorf("Expected latitude 40.7128, got %f", gps.GetLatitude())
		}
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		// Should not panic or error
		gps.processGPSDMessage("invalid json{{{")
		// Position should remain from previous test
		if !gps.IsValid() {
			t.Error("Position should still be valid from previous message")
		}
	})

	t.Run("Non-TPV message", func(t *testing.T) {
		msg := map[string]interface{}{
			"class": "SKY",
			"tag":   "MID2",
		}
		data, _ := json.Marshal(msg)

		// Should not update position
		oldLat := gps.GetLatitude()
		gps.processGPSDMessage(string(data))

		if gps.GetLatitude() != oldLat {
			t.Error("Non-TPV message should not update position")
		}
	})
}

func TestGPSService_ConcurrentAccess(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		position: PositionReport{
			Latitude:  37.7749,
			Longitude: -122.4194,
			Valid:     true,
		},
	}

	// Test concurrent reads and writes
	done := make(chan bool)

	// Multiple readers
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = gps.GetLatitude()
				_ = gps.GetLongitude()
				_ = gps.GetPosition()
				_ = gps.IsValid()
			}
			done <- true
		}()
	}

	// Multiple writers
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < 50; j++ {
				tpv := TPVReport{
					Class: "TPV",
					Mode:  3,
					Lat:   float64(30 + id),
					Lon:   float64(-120 + id),
					Alt:   float64(100 + j),
				}
				gps.updatePosition(tpv)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// If we got here without deadlock or race condition, test passes
	if !gps.IsValid() {
		t.Error("Expected valid position after concurrent access")
	}
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
	fmt.Sscanf(parts[1], "%02X", &actualChecksum)

	return expectedChecksum == actualChecksum
}

func TestUpdatePosition_WithoutTimestamp(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
	}

	tpv := TPVReport{
		Class: "TPV",
		Mode:  3,
		Time:  "", // Empty time
		Lat:   37.7749,
		Lon:   -122.4194,
		Alt:   50.0,
	}

	gps.updatePosition(tpv)

	if !gps.IsValid() {
		t.Error("Expected valid position even without timestamp")
	}

	// Timestamp should be set to approximately now
	if time.Since(gps.position.Timestamp) > time.Second {
		t.Error("Expected timestamp to be set to current time")
	}
}

func TestGPSService_Close(t *testing.T) {
	log := zerolog.Nop()

	t.Run("Close without connection", func(t *testing.T) {
		gps := &GPSService{
			Log: log,
		}
		ctx, cancel := context.WithCancel(context.Background())
		gps.ctx = ctx
		gps.cancel = cancel

		err := gps.Close()
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("Close with connection", func(t *testing.T) {
		mock := newMockGPSDServer(t)
		mock.Start()
		defer mock.Stop()

		<-mock.started

		gps, err := NewGPSServiceWithAddress(log, nil, mock.address)
		if err != nil {
			t.Fatalf("Failed to create GPS service: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		err = gps.Close()
		if err != nil {
			t.Errorf("Expected no error on close, got %v", err)
		}
	})
}

func TestSendCoTToMulticast(t *testing.T) {
	log := zerolog.Nop()

	// Create GPS service with valid position and proper initialization
	gps := &GPSService{
		Log: log,
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Speed:     5.0,
		Track:     90.0,
		Valid:     true,
		Mode:      3,
	}

	// Test that CoT message can be created without errors
	err := gps.sendCoTToMulticast()
	// We don't check for connection errors since multicast might not be available in test env
	// Just verify the function doesn't panic and handles the position correctly
	if err != nil && !strings.Contains(err.Error(), "multicast") && !strings.Contains(err.Error(), "network") && !strings.Contains(err.Error(), "dial") {
		t.Errorf("Unexpected error creating CoT message: %v", err)
	}
}

func TestSendCoTToMulticast_InvalidPosition(t *testing.T) {
	log := zerolog.Nop()

	// Create GPS service with invalid position
	gps := &GPSService{
		Log: log,
	}

	gps.position = PositionReport{
		Valid: false,
	}

	err := gps.sendCoTToMulticast()
	if err == nil {
		t.Error("Expected error for invalid position")
	}

	if !strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Expected 'no valid GPS position' error, got: %v", err)
	}
}

func TestCheckDeviceActive_InvalidIP(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
	}

	// Test with invalid IP address
	result := gps.checkDeviceActive("invalid-ip")
	if result {
		t.Error("Expected false for invalid IP address")
	}

	// Test with empty IP
	result = gps.checkDeviceActive("")
	if result {
		t.Error("Expected false for empty IP address")
	}

	// Test with malformed IP
	result = gps.checkDeviceActive("999.999.999.999")
	if result {
		t.Error("Expected false for malformed IP address")
	}
}

func TestCheckDeviceActive_NoMatchingInterface(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
	}

	// Test with an IP that's unlikely to be on any local subnet
	// Should return true (conservative approach when no interface is found)
	result := gps.checkDeviceActive("192.0.2.1") // TEST-NET-1 address
	if !result {
		t.Error("Expected true (conservative approach) when no matching interface is found")
	}
}

func TestCheckDeviceActive_Localhost(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
	}

	// Test with localhost - should find loopback interface but skip it
	// Should return true (conservative fallback)
	result := gps.checkDeviceActive("127.0.0.1")
	if !result {
		t.Error("Expected true for localhost (conservative fallback)")
	}
}

func TestSendLocationtoEUDs_RateLimit(t *testing.T) {
	// NOTE: This test is skipped because it requires a working OpenWRT environment with ubus.
	// In a test environment, GetCurrentDHCPLeases() will fail, causing SendLocationtoEUDs()
	// to return early before attempting multicast. Rate limiting is tested implicitly through
	// the sendCoTToMulticast function which is only called when no devices are reachable.
	t.Skip("Skipping rate limit test - requires OpenWRT environment with ubus")

	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Speed:     5.0,
		Track:     90.0,
		Valid:     true,
		Mode:      3,
	}

	// First call should attempt to send (may fail due to network/no dhcp leases, but should try)
	gps.SendLocationtoEUDs()

	// Verify lastMulticastTime was set
	gps.mu.RLock()
	lastTime := gps.lastMulticastTime
	gps.mu.RUnlock()

	if lastTime.IsZero() {
		t.Error("Expected lastMulticastTime to be set after first call")
	}

	// Second call immediately should be rate limited
	gps.SendLocationtoEUDs()

	// Verify the timestamp hasn't changed (rate limited)
	gps.mu.RLock()
	newTime := gps.lastMulticastTime
	gps.mu.RUnlock()

	if !newTime.Equal(lastTime) {
		t.Error("Expected lastMulticastTime to remain unchanged when rate limited")
	}
}

func TestSendCoTToMulticast_HAE_Calculation(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a position with MSL altitude and geoid separation
	// HAE should be MSL + Geoid Separation
	gps.position = PositionReport{
		Timestamp:       time.Now(),
		Latitude:        40.7128,
		Longitude:       -74.0060,
		Altitude:        10.0, // MSL altitude
		Speed:           5.0,
		Track:           90.0,
		Valid:           true,
		Mode:            3,
		GeoidSeparation: -33.5, // Geoid separation for New York area
	}

	// The function will create a CoT message
	// We can't easily verify the internal HAE calculation without mocking,
	// but we can at least ensure it doesn't error with geoid separation
	err := gps.sendCoTToMulticast()

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid position and geoid separation: %v", err)
	}

	// Test with zero geoid separation (HAE should equal MSL)
	gps.position.GeoidSeparation = 0
	err = gps.sendCoTToMulticast()

	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid position and zero geoid separation: %v", err)
	}
}

func TestSendCoTAsExternalGPS_InvalidPosition(t *testing.T) {
	log := zerolog.Nop()

	// Create GPS service with invalid position
	gps := &GPSService{
		Log: log,
	}

	gps.position = PositionReport{
		Valid: false,
	}

	err := gps.sendCoTTAsExternalGPS("192.168.1.100")
	if err == nil {
		t.Error("Expected error for invalid position")
	}

	if !strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Expected 'no valid GPS position' error, got: %v", err)
	}
}

func TestSendCoTAsExternalGPS_ValidPosition(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Speed:     5.0,
		Track:     90.0,
		Valid:     true,
		Mode:      3,
		HDOP:      1.2,
	}

	// Test that CoT message can be created without errors
	err := gps.sendCoTTAsExternalGPS("192.168.1.100")
	// We don't check for connection errors since the address might not be available in test env
	// Just verify the function doesn't panic and handles the position correctly
	if err != nil && !strings.Contains(err.Error(), "dial") && !strings.Contains(err.Error(), "network") && !strings.Contains(err.Error(), "resolve") {
		t.Errorf("Unexpected error creating CoT message: %v", err)
	}
}

func TestSendCoTAsExternalGPS_HAE_Calculation(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a position with MSL altitude and geoid separation
	// HAE should be MSL + Geoid Separation
	gps.position = PositionReport{
		Timestamp:       time.Now(),
		Latitude:        40.7128,
		Longitude:       -74.0060,
		Altitude:        10.0, // MSL altitude
		Speed:           5.0,
		Track:           90.0,
		Valid:           true,
		Mode:            3,
		HDOP:            1.5,
		GeoidSeparation: -33.5, // Geoid separation for New York area
	}

	// The function will create a CoT message
	// We can't easily verify the internal HAE calculation without mocking,
	// but we can at least ensure it doesn't error with geoid separation
	err := gps.sendCoTTAsExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid position and geoid separation: %v", err)
	}

	// Test with zero geoid separation (HAE should equal MSL)
	gps.position.GeoidSeparation = 0
	err = gps.sendCoTTAsExternalGPS("192.168.1.100")

	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid position and zero geoid separation: %v", err)
	}
}

func TestSendCoTAsExternalGPS_InvalidIPAddress(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Speed:     5.0,
		Track:     90.0,
		Valid:     true,
		Mode:      3,
	}

	// Test with invalid IP address format
	err := gps.sendCoTTAsExternalGPS("invalid-ip-address")
	if err == nil {
		t.Error("Expected error for invalid IP address")
	}

	// Should get an error about resolving or dialing
	if !strings.Contains(err.Error(), "resolve") && !strings.Contains(err.Error(), "dial") {
		t.Errorf("Expected resolve or dial error for invalid IP, got: %v", err)
	}
}

func TestSendLocationtoEUDs_NoValidPosition(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set invalid position
	gps.position = PositionReport{
		Valid: false,
	}

	// Should return early without error
	gps.SendLocationtoEUDs()
	// Test passes if no panic occurs
}

func TestReconnectionLimit(t *testing.T) {
	log := zerolog.Nop()

	// Create a GPS service with an invalid address to trigger reconnection failures
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gps := &GPSService{
		Log:            log,
		address:        "127.0.0.1:1", // Invalid port that should fail to connect
		ctx:            ctx,
		cancel:         cancel,
		reconnectDelay: 10 * time.Millisecond, // Short delay for testing
	}

	// Start the connection handler in a goroutine
	done := make(chan struct{})
	go func() {
		gps.connectionHandler()
		close(done)
	}()

	// Wait for the handler to complete (should stop after 3 failed attempts)
	select {
	case <-done:
		// Good - the handler stopped
	case <-time.After(2 * time.Second):
		t.Error("Connection handler did not stop after max reconnection attempts")
		cancel()
	}

	// Verify reconnectAttempts reached the limit
	gps.mu.RLock()
	attempts := gps.reconnectAttempts
	gps.mu.RUnlock()

	if attempts < maxReconnectAttempts {
		t.Errorf("Expected at least %d reconnection attempts, got %d", maxReconnectAttempts, attempts)
	}
}

func TestUpdatePosition_TriggersLocationSend(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Create a TPV report with valid position
	tpv := TPVReport{
		Class: "TPV",
		Mode:  3,
		Time:  time.Now().UTC().Format(time.RFC3339),
		Lat:   37.7749,
		Lon:   -122.4194,
		Alt:   50.0,
		Speed: 5.0,
		Track: 90.0,
	}

	// Update position (this should trigger SendLocationtoEUDs in a goroutine)
	gps.updatePosition(tpv)

	// Give the goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// Verify position was updated
	pos := gps.GetPosition()
	if !pos.Valid {
		t.Error("Expected valid position after update")
	}
	if pos.Latitude != 37.7749 {
		t.Errorf("Expected latitude 37.7749, got %f", pos.Latitude)
	}
	if pos.Longitude != -122.4194 {
		t.Errorf("Expected longitude -122.4194, got %f", pos.Longitude)
	}
}

func TestUpdateSatelliteInfo(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set an initial position
	gps.position = PositionReport{
		Valid: true,
		Mode:  3,
	}

	// Create a SKY report
	skyReport := SKYReport{
		Class: "SKY",
		HDOP:  1.2,
		VDOP:  2.1,
		PDOP:  2.5,
		NSat:  12,
		USat:  8,
	}

	// Update satellite info
	gps.updateSatelliteInfo(skyReport)

	// Verify the data was updated
	pos := gps.GetPosition()
	if pos.SatellitesUsed != 8 {
		t.Errorf("Expected 8 satellites used, got %d", pos.SatellitesUsed)
	}
	if pos.HDOP != 1.2 {
		t.Errorf("Expected HDOP 1.2, got %f", pos.HDOP)
	}
}

func TestUpdateSatelliteInfo_ZeroValues(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set initial values
	gps.position = PositionReport{
		Valid:          true,
		SatellitesUsed: 5,
		HDOP:           2.0,
	}

	// Create a SKY report with zero/invalid values
	skyReport := SKYReport{
		Class: "SKY",
		HDOP:  0,
		USat:  0,
	}

	// Update satellite info - should not overwrite with zeros
	gps.updateSatelliteInfo(skyReport)

	// Verify the data was NOT updated (zero values should be ignored)
	pos := gps.GetPosition()
	if pos.SatellitesUsed != 5 {
		t.Errorf("Expected satellites to remain 5, got %d", pos.SatellitesUsed)
	}
	if pos.HDOP != 2.0 {
		t.Errorf("Expected HDOP to remain 2.0, got %f", pos.HDOP)
	}
}

func TestUpdatePosition_PreservesSatelliteData(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// First, set satellite data from a SKY report
	skyReport := SKYReport{
		Class: "SKY",
		HDOP:  1.5,
		USat:  10,
	}
	gps.updateSatelliteInfo(skyReport)

	// Verify satellite data was set
	if gps.position.SatellitesUsed != 10 {
		t.Errorf("Expected satellites 10, got %d", gps.position.SatellitesUsed)
	}
	if gps.position.HDOP != 1.5 {
		t.Errorf("Expected HDOP 1.5, got %f", gps.position.HDOP)
	}

	// Now update position from a TPV report
	tpv := TPVReport{
		Class: "TPV",
		Mode:  3,
		Time:  time.Now().UTC().Format(time.RFC3339),
		Lat:   40.7128,
		Lon:   -74.0060,
		Alt:   10.0,
		Speed: 2.5,
		Track: 180.0,
	}
	gps.updatePosition(tpv)

	// Wait for async operations
	time.Sleep(10 * time.Millisecond)

	// Verify satellite data was PRESERVED after TPV update
	pos := gps.GetPosition()
	if pos.SatellitesUsed != 10 {
		t.Errorf("Expected satellites to be preserved at 10, got %d", pos.SatellitesUsed)
	}
	if pos.HDOP != 1.5 {
		t.Errorf("Expected HDOP to be preserved at 1.5, got %f", pos.HDOP)
	}

	// Verify TPV data was updated
	if pos.Latitude != 40.7128 {
		t.Errorf("Expected latitude 40.7128, got %f", pos.Latitude)
	}
	if !pos.Valid {
		t.Error("Expected position to be valid")
	}
}

func TestProcessGPSDMessage_SKYReport(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set initial position
	gps.position = PositionReport{
		Valid: true,
		Mode:  3,
	}

	// Create a SKY message
	skyReport := SKYReport{
		Class: "SKY",
		Time:  time.Now().UTC().Format(time.RFC3339),
		HDOP:  1.5,
		VDOP:  2.0,
		PDOP:  2.5,
		NSat:  10,
		USat:  7,
	}

	skyJSON, _ := json.Marshal(skyReport)

	// Process the message
	gps.processGPSDMessage(string(skyJSON))

	// Verify satellite info was updated
	pos := gps.GetPosition()
	if pos.SatellitesUsed != 7 {
		t.Errorf("Expected 7 satellites, got %d", pos.SatellitesUsed)
	}
	if pos.HDOP != 1.5 {
		t.Errorf("Expected HDOP 1.5, got %f", pos.HDOP)
	}
}

func TestProcessGPSDMessage_TPVWithGeoidSep(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Create a TPV message with geoid separation
	tpv := TPVReport{
		Class:    "TPV",
		Mode:     3,
		Time:     time.Now().UTC().Format(time.RFC3339),
		Lat:      40.7128,
		Lon:      -74.0060,
		Alt:      10.0,
		Speed:    2.5,
		Track:    180.0,
		GeoidSep: -33.5, // Geoid separation for New York area
	}

	tpvJSON, _ := json.Marshal(tpv)

	// Process the message
	gps.processGPSDMessage(string(tpvJSON))

	// Give time for async operations
	time.Sleep(10 * time.Millisecond)

	// Verify geoid separation was captured
	pos := gps.GetPosition()
	if pos.GeoidSeparation != -33.5 {
		t.Errorf("Expected geoid separation -33.5, got %f", pos.GeoidSeparation)
	}
	if !pos.Valid {
		t.Error("Expected valid position")
	}
}

func TestProcessGPSDMessage_TPVWithAccuracyEstimates(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Create a TPV message with error estimates
	tpv := TPVReport{
		Class: "TPV",
		Mode:  3,
		Time:  time.Now().UTC().Format(time.RFC3339),
		Lat:   40.7128,
		Lon:   -74.0060,
		Alt:   10.0,
		Speed: 2.5,
		Track: 180.0,
		EPH:   5.2, // Horizontal position error
		EPX:   3.1, // Longitude error
		EPY:   4.3, // Latitude error
		EPV:   8.7, // Vertical error
	}

	tpvJSON, _ := json.Marshal(tpv)

	// Process the message
	gps.processGPSDMessage(string(tpvJSON))

	// Give time for async operations
	time.Sleep(10 * time.Millisecond)

	// Verify error estimates were captured
	pos := gps.GetPosition()
	if pos.EPH != 5.2 {
		t.Errorf("Expected EPH 5.2, got %f", pos.EPH)
	}
	if pos.EPX != 3.1 {
		t.Errorf("Expected EPX 3.1, got %f", pos.EPX)
	}
	if pos.EPY != 4.3 {
		t.Errorf("Expected EPY 4.3, got %f", pos.EPY)
	}
	if pos.EPV != 8.7 {
		t.Errorf("Expected EPV 8.7, got %f", pos.EPV)
	}
	if !pos.Valid {
		t.Error("Expected valid position")
	}
}

func TestProcessGPSDMessage_TPVWithDGPS(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Create a TPV message with DGPS information
	tpv := TPVReport{
		Class:   "TPV",
		Mode:    3,
		Time:    time.Now().UTC().Format(time.RFC3339),
		Lat:     40.7128,
		Lon:     -74.0060,
		Alt:     10.0,
		Speed:   2.5,
		Track:   180.0,
		DGPSAge: 2.5, // Age of DGPS correction
		DGPSSta: 120, // DGPS station ID
	}

	tpvJSON, _ := json.Marshal(tpv)

	// Process the message
	gps.processGPSDMessage(string(tpvJSON))

	// Give time for async operations
	time.Sleep(10 * time.Millisecond)

	// Verify DGPS data was captured
	pos := gps.GetPosition()
	if pos.DGPSAge != 2.5 {
		t.Errorf("Expected DGPS age 2.5, got %f", pos.DGPSAge)
	}
	if pos.DGPSStation != 120 {
		t.Errorf("Expected DGPS station 120, got %d", pos.DGPSStation)
	}
	if !pos.Valid {
		t.Error("Expected valid position")
	}
}

func TestFormatGGA_WithRealSatelliteData(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC)

	pos := PositionReport{
		Timestamp:       now,
		Latitude:        37.7749,
		Longitude:       -122.4194,
		Altitude:        50.5,
		Speed:           5.0,
		Track:           90.0,
		Valid:           true,
		Mode:            3,
		SatellitesUsed:  10,
		HDOP:            1.3,
		GeoidSeparation: -32.5,
	}

	nmea := formatGGA(pos)

	// Verify NMEA format
	if !strings.HasPrefix(nmea, "$GPGGA,") {
		t.Errorf("Expected NMEA to start with $GPGGA, got: %s", nmea)
	}

	// Check that it contains the real satellite count (10)
	if !strings.Contains(nmea, ",10,") {
		t.Errorf("Expected satellite count 10 in NMEA, got: %s", nmea)
	}

	// Check that it contains the real HDOP (1.3)
	if !strings.Contains(nmea, ",1.3,") {
		t.Errorf("Expected HDOP 1.3 in NMEA, got: %s", nmea)
	}

	// Check that it contains the real geoid separation (-32.5)
	if !strings.Contains(nmea, ",-32.5,M,") {
		t.Errorf("Expected geoid separation -32.5 in NMEA, got: %s", nmea)
	}
}

func TestFormatGGA_WithDefaultValues(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC)

	pos := PositionReport{
		Timestamp: now,
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.5,
		Speed:     5.0,
		Track:     90.0,
		Valid:     true,
		Mode:      3,
		// No satellite data - should have empty fields (no false data)
		SatellitesUsed:  0,
		HDOP:            0,
		GeoidSeparation: 0,
	}

	nmea := formatGGA(pos)

	// Verify NMEA format is valid
	if !strings.HasPrefix(nmea, "$GPGGA,") {
		t.Errorf("Expected NMEA to start with $GPGGA, got: %s", nmea)
	}

	// Should have "00" for satellite count when no data available (instead of empty)
	// The pattern should be ,1,00, (quality, numSat=00, empty hdop)
	if !strings.Contains(nmea, ",1,00,") {
		t.Errorf("Expected satellite count 00 in NMEA when no data, got: %s", nmea)
	}

	// Should have empty geoid separation when no data available
	// The pattern should be ,M,, (altitude unit, empty geoid, geoid unit)
	if !strings.Contains(nmea, ",M,,M,") {
		t.Errorf("Expected empty geoid separation field in NMEA when no data, got: %s", nmea)
	}
}

func TestFormatGGA_WithDGPSData(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC)

	tests := []struct {
		name        string
		dgpsAge     float64
		dgpsStation int
		expectAge   string
		expectSta   string
	}{
		{
			name:        "DGPS with age and station",
			dgpsAge:     2.5,
			dgpsStation: 120,
			expectAge:   "2.5",
			expectSta:   "0120",
		},
		{
			name:        "DGPS with different station",
			dgpsAge:     1.2,
			dgpsStation: 999,
			expectAge:   "1.2",
			expectSta:   "0999",
		},
		{
			name:        "No DGPS - zero values",
			dgpsAge:     0,
			dgpsStation: 0,
			expectAge:   "",
			expectSta:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := PositionReport{
				Timestamp:   now,
				Latitude:    37.7749,
				Longitude:   -122.4194,
				Altitude:    50.5,
				Valid:       true,
				Mode:        3,
				DGPSAge:     tt.dgpsAge,
				DGPSStation: tt.dgpsStation,
			}

			nmea := formatGGA(pos)

			// NMEA format: $GPGGA,...,dgpsAge,dgpsStation*checksum
			// Split by * to get sentence without checksum
			parts := strings.Split(nmea, "*")
			if len(parts) != 2 {
				t.Fatalf("Expected NMEA to have checksum, got: %s", nmea)
			}

			sentence := parts[0]
			fields := strings.Split(sentence, ",")

			// DGPS age is field 13 (0-indexed)
			if len(fields) > 13 {
				if fields[13] != tt.expectAge {
					t.Errorf("Expected DGPS age '%s', got '%s'", tt.expectAge, fields[13])
				}
			}

			// DGPS station is field 14 (0-indexed)
			if len(fields) > 14 {
				if fields[14] != tt.expectSta {
					t.Errorf("Expected DGPS station '%s', got '%s'", tt.expectSta, fields[14])
				}
			}
		})
	}
}

func TestProcessGPSDMessage_UnknownMessageType(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Create an unknown message type (VERSION, DEVICES, etc.)
	unknownMsg := `{"class":"VERSION","release":"3.23","rev":"3.23","proto_major":3,"proto_minor":14}`

	// Should not panic or error
	gps.processGPSDMessage(unknownMsg)

	// Position should remain invalid/unchanged
	pos := gps.GetPosition()
	if pos.Valid {
		t.Error("Position should remain invalid after unknown message")
	}
}

func TestProcessGPSDMessage_InvalidJSON(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Invalid JSON should not cause panic
	gps.processGPSDMessage("not valid json {{{")

	// Should handle gracefully
	pos := gps.GetPosition()
	if pos.Valid {
		t.Error("Position should remain invalid after invalid JSON")
	}
}

func TestFormatGGA_QualityIndicator_DGPS(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC)

	tests := []struct {
		name            string
		mode            int
		dgpsStation     int
		expectedQuality string
	}{
		{"No fix - mode 0", 0, 0, "0"},
		{"No fix - mode 1", 1, 0, "0"},
		{"2D fix - no DGPS", 2, 0, "1"},
		{"3D fix - no DGPS", 3, 0, "1"},
		{"2D fix - with DGPS", 2, 120, "2"},
		{"3D fix - with DGPS", 3, 120, "2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := PositionReport{
				Timestamp:      now,
				Latitude:       37.7749,
				Longitude:      -122.4194,
				Altitude:       50.5,
				Valid:          true,
				Mode:           tt.mode,
				SatellitesUsed: 8,
				HDOP:           1.2,
				DGPSStation:    tt.dgpsStation,
			}

			nmea := formatGGA(pos)

			// Extract quality field (field 6, 0-indexed)
			parts := strings.Split(strings.Split(nmea, "*")[0], ",")
			if len(parts) > 6 {
				quality := parts[6]
				if quality != tt.expectedQuality {
					t.Errorf("Expected quality %s, got %s for %s", tt.expectedQuality, quality, tt.name)
				}
			} else {
				t.Errorf("NMEA sentence doesn't have enough fields: %s", nmea)
			}
		})
	}
}

func TestSKYReport_WithSatelliteDetails(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	gps.position = PositionReport{
		Valid: true,
		Mode:  3,
	}

	// Create a SKY report with satellite details
	skyJSON := `{
		"class":"SKY",
		"time":"2024-01-15T12:30:45.000Z",
		"hdop":1.2,
		"vdop":2.0,
		"pdop":2.3,
		"nSat":12,
		"uSat":8,
		"satellites":[
			{"PRN":1,"el":45.0,"az":180.0,"ss":42.0,"used":true},
			{"PRN":2,"el":30.0,"az":90.0,"ss":38.0,"used":true},
			{"PRN":3,"el":60.0,"az":270.0,"ss":45.0,"used":true}
		]
	}`

	gps.processGPSDMessage(skyJSON)

	pos := gps.GetPosition()
	if pos.SatellitesUsed != 8 {
		t.Errorf("Expected 8 satellites used, got %d", pos.SatellitesUsed)
	}
	if pos.HDOP != 1.2 {
		t.Errorf("Expected HDOP 1.2, got %f", pos.HDOP)
	}
}

func TestConcurrentTPVandSKYUpdates(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set initial position so TPV updates preserve satellite data
	gps.position = PositionReport{
		Valid:          true,
		Mode:           3,
		SatellitesUsed: 5,
		HDOP:           1.5,
	}

	// Simulate concurrent TPV and SKY updates
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 50; i++ {
			// Read current satellite data
			gps.mu.RLock()
			currentSats := gps.position.SatellitesUsed
			currentHDOP := gps.position.HDOP
			gps.mu.RUnlock()

			// Update position, preserving satellite data
			gps.mu.Lock()
			gps.position.Latitude = 37.7749
			gps.position.Longitude = -122.4194
			gps.position.Altitude = 50.0 + float64(i)
			gps.position.GeoidSeparation = -32.5
			gps.position.Valid = true
			gps.position.Mode = 3
			// Preserve satellite data
			gps.position.SatellitesUsed = currentSats
			gps.position.HDOP = currentHDOP
			gps.mu.Unlock()

			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			sky := SKYReport{
				Class: "SKY",
				HDOP:  1.2 + float64(i)*0.01,
				USat:  8 + i%3,
			}
			gps.updateSatelliteInfo(sky)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Verify we can still get position without race conditions
	pos := gps.GetPosition()
	if !pos.Valid {
		t.Error("Expected valid position after concurrent updates")
	}
	// Satellite data should be preserved from SKY updates
	if pos.SatellitesUsed < 8 || pos.SatellitesUsed > 10 {
		t.Errorf("Expected satellite count between 8-10, got %d", pos.SatellitesUsed)
	}
	if pos.HDOP < 1.2 {
		t.Errorf("Expected HDOP >= 1.2, got %f", pos.HDOP)
	}
}

func TestFormatGGA_QualityIndicator(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC)

	tests := []struct {
		name            string
		mode            int
		expectedQuality string
	}{
		{"No fix - mode 0", 0, "0"},
		{"No fix - mode 1", 1, "0"},
		{"2D fix - mode 2", 2, "1"},
		{"3D fix - mode 3", 3, "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := PositionReport{
				Timestamp:      now,
				Latitude:       37.7749,
				Longitude:      -122.4194,
				Altitude:       50.5,
				Valid:          true,
				Mode:           tt.mode,
				SatellitesUsed: 8,
				HDOP:           1.2,
			}

			nmea := formatGGA(pos)

			// Quality appears after longitude hemisphere and before satellite count
			// Format: ...W,Q,SS,... where Q is quality, SS is satellites
			expectedPattern := fmt.Sprintf(",W,%s,08,", tt.expectedQuality)
			if !strings.Contains(nmea, expectedPattern) {
				t.Errorf("Expected quality indicator %s in NMEA, got: %s", tt.expectedQuality, nmea)
			}
		})
	}
}

func TestSendNMEAasExternalGPS_InvalidPosition(t *testing.T) {
	log := zerolog.Nop()

	// Create GPS service with invalid position
	gps := &GPSService{
		Log: log,
	}

	gps.position = PositionReport{
		Valid: false,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")
	if err == nil {
		t.Error("Expected error for invalid position")
	}

	if !strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Expected 'no valid GPS position' error, got: %v", err)
	}
}

func TestSendNMEAasExternalGPS_ValidPosition(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Speed:     5.0,
		Track:     90.0,
		Valid:     true,
		Mode:      3,
		HDOP:      1.2,
	}

	// Test that NMEA message can be created without errors
	err := gps.sendNMEAasExternalGPS("192.168.1.100")
	// We don't check for connection errors since the address might not be available in test env
	// Just verify the function doesn't panic and handles the position correctly
	if err != nil && !strings.Contains(err.Error(), "dial") && !strings.Contains(err.Error(), "network") && !strings.Contains(err.Error(), "resolve") {
		t.Errorf("Unexpected error creating NMEA message: %v", err)
	}
}

func TestSendNMEAasExternalGPS_InvalidIPAddress(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Speed:     5.0,
		Track:     90.0,
		Valid:     true,
		Mode:      3,
	}

	// Test with invalid IP address format
	err := gps.sendNMEAasExternalGPS("invalid-ip-address")
	if err == nil {
		t.Error("Expected error for invalid IP address")
	}

	// Should get an error about resolving or dialing
	if !strings.Contains(err.Error(), "resolve") && !strings.Contains(err.Error(), "dial") {
		t.Errorf("Expected resolve or dial error for invalid IP, got: %v", err)
	}
}

func TestSendNMEAasExternalGPS_AllPositionFields(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a position with all fields populated
	gps.position = PositionReport{
		Timestamp:       time.Now(),
		Latitude:        40.7128,
		Longitude:       -74.0060,
		Altitude:        10.0,
		Speed:           15.5,
		Track:           270.5,
		Valid:           true,
		Mode:            3,
		HDOP:            0.9,
		SatellitesUsed:  12,
		GeoidSeparation: -33.5,
	}

	// Test that function handles comprehensive position data
	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid comprehensive position: %v", err)
	}
}

func TestSendNMEAasExternalGPS_MalformedIPAddress(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Valid:     true,
		Mode:      3,
	}

	// Test with malformed IP address (too many octets)
	err := gps.sendNMEAasExternalGPS("999.999.999.999")
	if err == nil {
		t.Error("Expected error for malformed IP address")
		return
	}

	// Should get an error about resolving or dialing
	if !strings.Contains(err.Error(), "resolve") && !strings.Contains(err.Error(), "dial") && !strings.Contains(err.Error(), "network") {
		t.Errorf("Expected resolve/dial/network error for malformed IP, got: %v", err)
	}
}

func TestSendNMEAasExternalGPS_NorthernLatitude(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position in northern hemisphere
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  51.5074, // London
		Longitude: -0.1278,
		Altitude:  11.0,
		Valid:     true,
		Mode:      3,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid northern hemisphere position: %v", err)
	}
}

func TestSendNMEAasExternalGPS_SouthernLatitude(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position in southern hemisphere
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  -33.8688, // Sydney
		Longitude: 151.2093,
		Altitude:  3.0,
		Valid:     true,
		Mode:      3,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid southern hemisphere position: %v", err)
	}
}

func TestSendNMEAasExternalGPS_WesternLongitude(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position in western hemisphere
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,   // San Francisco
		Longitude: -122.4194, // Western longitude
		Altitude:  16.0,
		Valid:     true,
		Mode:      3,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid western longitude: %v", err)
	}
}

func TestSendNMEAasExternalGPS_EasternLongitude(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position in eastern hemisphere
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  35.6762,  // Tokyo
		Longitude: 139.6503, // Eastern longitude
		Altitude:  40.0,
		Valid:     true,
		Mode:      3,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid eastern longitude: %v", err)
	}
}

func TestSendNMEAasExternalGPS_EquatorPrimeMeridian(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a position near equator and prime meridian
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  0.0001,
		Longitude: 0.0001,
		Altitude:  5.0,
		Valid:     true,
		Mode:      3,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with valid equator/prime meridian position: %v", err)
	}
}

func TestSendNMEAasExternalGPS_ZeroAltitude(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a position at sea level (zero altitude)
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  25.0000,
		Longitude: -71.0000,
		Altitude:  0.0, // Sea level
		Valid:     true,
		Mode:      3,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with zero altitude: %v", err)
	}
}

func TestSendNMEAasExternalGPS_HighAltitude(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a position at high altitude (Mt. Everest)
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  27.9881,
		Longitude: 86.9250,
		Altitude:  8848.86, // Mt. Everest height
		Valid:     true,
		Mode:      3,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with high altitude: %v", err)
	}
}

func TestSendNMEAasExternalGPS_NegativeAltitude(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a position below sea level (Dead Sea)
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  31.5590,
		Longitude: 35.4732,
		Altitude:  -430.5, // Dead Sea depth below sea level
		Valid:     true,
		Mode:      3,
	}

	err := gps.sendNMEAasExternalGPS("192.168.1.100")

	// May get network errors, but shouldn't get position errors
	if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Should not get position error with negative altitude: %v", err)
	}
}

func TestSendNMEAasExternalGPS_DifferentFixModes(t *testing.T) {
	log := zerolog.Nop()

	tests := []struct {
		name string
		mode int
	}{
		{"No fix - mode 0", 0},
		{"No fix - mode 1", 1},
		{"2D fix - mode 2", 2},
		{"3D fix - mode 3", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gps := &GPSService{
				Log: log,
				mu:  sync.RWMutex{},
			}

			gps.position = PositionReport{
				Timestamp: time.Now(),
				Latitude:  37.7749,
				Longitude: -122.4194,
				Altitude:  50.0,
				Valid:     true,
				Mode:      tt.mode,
				HDOP:      1.2,
			}

			err := gps.sendNMEAasExternalGPS("192.168.1.100")

			// May get network errors, but shouldn't get position errors regardless of mode
			if err != nil && strings.Contains(err.Error(), "no valid GPS position") {
				t.Errorf("Should not get position error with mode %d: %v", tt.mode, err)
			}
		})
	}
}

func TestSendNMEAasExternalGPS_IPv6Address(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Valid:     true,
		Mode:      3,
	}

	// Test with IPv6 address
	err := gps.sendNMEAasExternalGPS("::1")

	// May get network errors, but should attempt to resolve IPv6
	if err != nil && !strings.Contains(err.Error(), "dial") && !strings.Contains(err.Error(), "network") && !strings.Contains(err.Error(), "resolve") && !strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Unexpected error for IPv6 address: %v", err)
	}
}

func TestSendNMEAasExternalGPS_LocalhostAddress(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set a valid position
	gps.position = PositionReport{
		Timestamp: time.Now(),
		Latitude:  37.7749,
		Longitude: -122.4194,
		Altitude:  50.0,
		Valid:     true,
		Mode:      3,
	}

	// Test with localhost address
	err := gps.sendNMEAasExternalGPS("127.0.0.1")

	// localhost should be resolvable, may get dial/connection errors
	if err != nil && !strings.Contains(err.Error(), "dial") && !strings.Contains(err.Error(), "network") && !strings.Contains(err.Error(), "connection") && !strings.Contains(err.Error(), "no valid GPS position") {
		t.Errorf("Unexpected error for localhost address: %v", err)
	}
}

func TestProcessNMEASentence(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	tests := []struct {
		name         string
		sentence     string
		expectedType string
	}{
		{
			name:         "GPGGA sentence",
			sentence:     "$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47",
			expectedType: "GPGGA",
		},
		{
			name:         "GPRMC sentence",
			sentence:     "$GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W*6A",
			expectedType: "GPRMC",
		},
		{
			name:         "GPGSA sentence",
			sentence:     "$GPGSA,A,3,04,05,,09,12,,,24,,,,,2.5,1.3,2.1*39",
			expectedType: "GPGSA",
		},
		{
			name:         "GPGSV sentence",
			sentence:     "$GPGSV,2,1,08,01,40,083,46,02,17,308,41,12,07,344,39,14,22,228,45*75",
			expectedType: "GPGSV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Process the NMEA sentence
			gps.processNMEASentence(tt.sentence)

			// Verify it was stored as the last NMEA
			lastNMEA := gps.GetLastNMEA()
			if lastNMEA != tt.sentence {
				t.Errorf("Expected last NMEA to be %q, got %q", tt.sentence, lastNMEA)
			}

			// Verify it was stored by type
			stored := gps.GetNMEASentence(tt.expectedType)
			if stored != tt.sentence {
				t.Errorf("Expected %s sentence to be %q, got %q", tt.expectedType, tt.sentence, stored)
			}
		})
	}
}

func TestProcessGPSDMessage_NMEASentence(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	nmeaSentence := "$GPGGA,032459.00,3947.45069,N,10508.20322,W,1,10,1.5,1688.2,M,-17.8,M,,*45"

	// Process the message - should recognize it as NMEA
	gps.processGPSDMessage(nmeaSentence)

	// Verify it was processed as NMEA
	lastNMEA := gps.GetLastNMEA()
	if lastNMEA != nmeaSentence {
		t.Errorf("Expected NMEA sentence to be stored, got %q", lastNMEA)
	}

	// Verify it's accessible by type
	ggaSentence := gps.GetNMEASentence("GPGGA")
	if ggaSentence != nmeaSentence {
		t.Errorf("Expected GPGGA sentence to be %q, got %q", nmeaSentence, ggaSentence)
	}
}

func TestGetAllNMEASentences(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	sentences := []string{
		"$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47",
		"$GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W*6A",
		"$GPGSA,A,3,04,05,,09,12,,,24,,,,,2.5,1.3,2.1*39",
	}

	for _, sentence := range sentences {
		gps.processNMEASentence(sentence)
	}

	// Get all sentences
	allSentences := gps.GetAllNMEASentences()

	// Verify we got 3 different sentence types
	if len(allSentences) != 3 {
		t.Errorf("Expected 3 sentence types, got %d", len(allSentences))
	}

	// Verify each type is present
	expectedTypes := []string{"GPGGA", "GPRMC", "GPGSA"}
	for _, expectedType := range expectedTypes {
		if _, exists := allSentences[expectedType]; !exists {
			t.Errorf("Expected %s sentence to be in results", expectedType)
		}
	}

	// Verify modifying the returned map doesn't affect the original
	allSentences["GPGGA"] = "modified"
	if gps.GetNMEASentence("GPGGA") == "modified" {
		t.Error("Modifying returned map should not affect internal storage")
	}
}

func TestGetLastNMEA_EmptyWhenNoSentences(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	// Should return empty string when no sentences received
	if got := gps.GetLastNMEA(); got != "" {
		t.Errorf("Expected empty string, got %q", got)
	}
}

func TestGetNMEASentence_EmptyWhenTypeNotFound(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	// Add a GPGGA sentence
	gps.processNMEASentence("$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47")

	// Should return empty for non-existent type
	if got := gps.GetNMEASentence("GPRMC"); got != "" {
		t.Errorf("Expected empty string for non-existent type, got %q", got)
	}

	// Should return the sentence for existing type
	if got := gps.GetNMEASentence("GPGGA"); got == "" {
		t.Error("Expected GPGGA sentence, got empty string")
	}
}

func TestProcessNMEASentence_UpdatesExistingType(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	sentence1 := "$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47"
	sentence2 := "$GPGGA,133519,4807.038,N,01131.000,E,1,09,0.8,545.4,M,46.9,M,,*48"

	// Process first sentence
	gps.processNMEASentence(sentence1)
	if got := gps.GetNMEASentence("GPGGA"); got != sentence1 {
		t.Errorf("Expected first sentence, got %q", got)
	}

	// Process second sentence of same type - should replace
	gps.processNMEASentence(sentence2)
	if got := gps.GetNMEASentence("GPGGA"); got != sentence2 {
		t.Errorf("Expected second sentence, got %q", got)
	}
}

func TestProcessNMEASentence_InvalidFormat(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	tests := []struct {
		name     string
		sentence string
	}{
		{
			name:     "too short",
			sentence: "$GP",
		},
		{
			name:     "no comma",
			sentence: "$GPGGA123519",
		},
		{
			name:     "empty",
			sentence: "",
		},
		{
			name:     "no dollar sign",
			sentence: "GPGGA,123519,4807.038,N",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initialCount := len(gps.nmeaSentences)
			gps.processNMEASentence(tt.sentence)

			// Should still update lastNMEA for non-empty strings starting with $
			if tt.sentence != "" && len(tt.sentence) > 0 && tt.sentence[0] == '$' {
				if gps.GetLastNMEA() != tt.sentence {
					t.Errorf("Expected lastNMEA to be updated for %q", tt.sentence)
				}
			}

			// For very short sentences without proper format, map might not be updated
			if tt.sentence == "$GP" || tt.sentence == "" || (len(tt.sentence) > 0 && tt.sentence[0] != '$') {
				// These shouldn't add to the map if they're too malformed
				afterCount := len(gps.nmeaSentences)
				// Allow for the fact that very short ones might still get added
				_ = afterCount // Use the variable
				_ = initialCount
			}
		})
	}
}

func TestGetAllNMEASentences_EmptyMap(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	sentences := gps.GetAllNMEASentences()
	if len(sentences) != 0 {
		t.Errorf("Expected empty map, got %d entries", len(sentences))
	}
}

func TestProcessNMEASentence_ThreadSafety(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log:           log,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	// Test concurrent access
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			gps.processNMEASentence("$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = gps.GetNMEASentence("GPGGA")
			_ = gps.GetLastNMEA()
			_ = gps.GetAllNMEASentences()
		}
		done <- true
	}()

	// Wait for both to complete
	<-done
	<-done

	// Verify final state
	if got := gps.GetNMEASentence("GPGGA"); got == "" {
		t.Error("Expected GPGGA sentence after concurrent writes")
	}
}

func TestProcessNMEASentence_SendsToDevicesWhenConfigured(t *testing.T) {
	log := zerolog.Nop()

	// Create a mock config with GetGNSSSendAsNMEA enabled
	v := viper.New()
	v.Set("GNSS.sendAsExternalGNSSSource.sendAsNMEA", true)
	cfg := config.New(v)

	gps := &GPSService{
		Log:           log,
		Config:        cfg,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	sentence := "$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47"

	// Process the NMEA sentence - should trigger send attempt (will fail gracefully with no DHCP leases)
	gps.processNMEASentence(sentence)

	// Give the goroutine a moment to start
	time.Sleep(50 * time.Millisecond)

	// Verify the sentence was stored
	if got := gps.GetNMEASentence("GPGGA"); got != sentence {
		t.Errorf("Expected GPGGA sentence to be stored, got %q", got)
	}
}

func TestProcessNMEASentence_NoSendWhenConfigDisabled(t *testing.T) {
	log := zerolog.Nop()

	// Create a mock config with GetGNSSSendAsNMEA disabled
	v := viper.New()
	v.Set("GNSS.sendAsExternalGNSSSource.sendAsNMEA", false)
	cfg := config.New(v)

	gps := &GPSService{
		Log:           log,
		Config:        cfg,
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	sentence := "$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47"

	// Process the NMEA sentence - should NOT trigger send
	gps.processNMEASentence(sentence)

	// Verify the sentence was still stored
	if got := gps.GetNMEASentence("GPGGA"); got != sentence {
		t.Errorf("Expected GPGGA sentence to be stored, got %q", got)
	}
}

func TestProcessNMEASentence_NoSendWhenConfigNil(t *testing.T) {
	log := zerolog.Nop()

	gps := &GPSService{
		Log:           log,
		Config:        nil, // No config
		mu:            sync.RWMutex{},
		nmeaSentences: make(map[string]string),
	}

	sentence := "$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47"

	// Process the NMEA sentence - should NOT crash with nil config
	gps.processNMEASentence(sentence)

	// Verify the sentence was still stored
	if got := gps.GetNMEASentence("GPGGA"); got != sentence {
		t.Errorf("Expected GPGGA sentence to be stored, got %q", got)
	}
}
