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

	"github.com/rs/zerolog"
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
	gps, err := NewGPSServiceWithAddress(log, mock.address)
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
	gps, err := NewGPSServiceWithAddress(log, mock.address)
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

func TestGPSService_InvalidPosition(t *testing.T) {
	log := zerolog.Nop()

	// Create a mock server with invalid data (mode 1 = no fix)
	mock := newMockGPSDServer(t)
	mock.AddTPVMessage(0, 0, 0, 0, 0, 1)
	mock.Start()
	defer mock.Stop()

	<-mock.started

	// Create GPS service
	gps, err := NewGPSServiceWithAddress(log, mock.address)
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

		gps, err := NewGPSServiceWithAddress(log, mock.address)
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

	// Should have empty satellite count when no data available (field between commas)
	// The pattern should be ,1,, (quality, empty numSat, empty hdop)
	if !strings.Contains(nmea, ",1,,") {
		t.Errorf("Expected empty satellite count field in NMEA when no data, got: %s", nmea)
	}

	// Should have empty geoid separation when no data available
	// The pattern should be ,M,, (altitude unit, empty geoid, geoid unit)
	if !strings.Contains(nmea, ",M,,M,") {
		t.Errorf("Expected empty geoid separation field in NMEA when no data, got: %s", nmea)
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
		Latitude:  37.7749,  // San Francisco
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
		Latitude:  35.6762, // Tokyo
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
