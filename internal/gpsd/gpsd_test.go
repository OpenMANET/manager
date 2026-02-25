package gpsd

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Integration tests that use multiple components together

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
