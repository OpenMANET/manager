package gpsd

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

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