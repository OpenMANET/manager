package gpsd

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// Tests for position.go functions:
// - updatePosition
// - updateSatelliteInfo
// - GetPosition
// - GetLatitude, GetLongitude, GetAltitude, GetSpeed, GetTrack
// - IsValid, GetMode
// - GetHorizontalAccuracy, GetVerticalAccuracy, GetLongitudeError, GetLatitudeError
// - GetDGPSAge, GetDGPSStation

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
