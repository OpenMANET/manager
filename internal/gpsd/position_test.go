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

func TestUpdatePosition_Mode0_InvalidFix(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
		position: PositionReport{
			Latitude:  10.0,
			Longitude: 20.0,
			Valid:     false,
		},
	}

	tpv := TPVReport{
		Class: "TPV",
		Mode:  0, // No fix
		Lat:   37.7749,
		Lon:   -122.4194,
		Alt:   100.0,
	}

	gps.updatePosition(tpv)

	// Position should NOT be updated for mode 0
	if gps.IsValid() {
		t.Error("Expected position to remain invalid for mode 0")
	}

	if gps.GetLatitude() != 10.0 {
		t.Errorf("Expected latitude to remain 10.0, got %f", gps.GetLatitude())
	}
}

func TestUpdatePosition_Mode1_InvalidFix(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
		position: PositionReport{
			Latitude:  10.0,
			Longitude: 20.0,
			Valid:     false,
		},
	}

	tpv := TPVReport{
		Class: "TPV",
		Mode:  1, // No fix
		Lat:   37.7749,
		Lon:   -122.4194,
		Alt:   100.0,
	}

	gps.updatePosition(tpv)

	// Position should NOT be updated for mode 1
	if gps.IsValid() {
		t.Error("Expected position to remain invalid for mode 1")
	}

	if gps.GetLatitude() != 10.0 {
		t.Errorf("Expected latitude to remain 10.0, got %f", gps.GetLatitude())
	}
}

func TestUpdatePosition_Mode2_2DFix(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	tpv := TPVReport{
		Class: "TPV",
		Mode:  2, // 2D fix
		Time:  time.Now().UTC().Format(time.RFC3339),
		Lat:   51.5074,
		Lon:   -0.1278,
		Alt:   0.0, // Altitude may be zero in 2D fix
		Speed: 3.0,
		Track: 270.0,
	}

	gps.updatePosition(tpv)

	// Wait for async goroutine
	time.Sleep(10 * time.Millisecond)

	if !gps.IsValid() {
		t.Error("Expected valid position for mode 2 (2D fix)")
	}

	if gps.GetMode() != 2 {
		t.Errorf("Expected mode 2, got %d", gps.GetMode())
	}

	if gps.GetLatitude() != 51.5074 {
		t.Errorf("Expected latitude 51.5074, got %f", gps.GetLatitude())
	}

	if gps.GetLongitude() != -0.1278 {
		t.Errorf("Expected longitude -0.1278, got %f", gps.GetLongitude())
	}
}

func TestUpdatePosition_WithAllErrorEstimates(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	tpv := TPVReport{
		Class:    "TPV",
		Mode:     3,
		Time:     time.Now().UTC().Format(time.RFC3339),
		Lat:      40.7128,
		Lon:      -74.0060,
		Alt:      10.0,
		Speed:    2.5,
		Track:    180.0,
		Climb:    -0.5,
		GeoidSep: -33.5,
		EPH:      5.2,
		EPX:      3.1,
		EPY:      4.3,
		EPV:      8.7,
		DGPSAge:  2.5,
		DGPSSta:  120,
	}

	gps.updatePosition(tpv)

	// Wait for async goroutine
	time.Sleep(10 * time.Millisecond)

	pos := gps.GetPosition()
	if !pos.Valid {
		t.Fatal("Expected valid position")
	}

	if pos.GeoidSeparation != -33.5 {
		t.Errorf("Expected geoid separation -33.5, got %f", pos.GeoidSeparation)
	}

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

	if pos.Climb != -0.5 {
		t.Errorf("Expected climb -0.5, got %f", pos.Climb)
	}

	if pos.DGPSAge != 2.5 {
		t.Errorf("Expected DGPS age 2.5, got %f", pos.DGPSAge)
	}

	if pos.DGPSStation != 120 {
		t.Errorf("Expected DGPS station 120, got %d", pos.DGPSStation)
	}
}

func TestUpdateSatelliteInfo_CachesFullReport(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	skyReport := SKYReport{
		Class: "SKY",
		HDOP:  1.2,
		VDOP:  1.8,
		PDOP:  2.4,
		NSat:  12,
		USat:  8,
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 2, El: 45.0, Az: 120.0, Ss: 38.0, Used: true},
			{PRN: 5, El: 72.0, Az: 210.0, Ss: 42.0, Used: true},
			{PRN: 7, El: 15.0, Az: 330.0, Ss: 18.0, Used: false},
		},
	}

	gps.updateSatelliteInfo(skyReport)

	report := gps.GetSatelliteReport()
	if report.HDOP != 1.2 {
		t.Errorf("Expected HDOP 1.2, got %f", report.HDOP)
	}

	if report.VDOP != 1.8 {
		t.Errorf("Expected VDOP 1.8, got %f", report.VDOP)
	}

	if report.PDOP != 2.4 {
		t.Errorf("Expected PDOP 2.4, got %f", report.PDOP)
	}

	if report.NSat != 12 {
		t.Errorf("Expected NSat 12, got %d", report.NSat)
	}

	if report.USat != 8 {
		t.Errorf("Expected USat 8, got %d", report.USat)
	}

	if len(report.Satellites) != 3 {
		t.Fatalf("Expected 3 satellites, got %d", len(report.Satellites))
	}

	// Verify first satellite
	sat := report.Satellites[0]
	if sat.PRN != 2 || sat.El != 45.0 || sat.Az != 120.0 || sat.Ss != 38.0 || !sat.Used {
		t.Errorf("First satellite mismatch: %+v", sat)
	}

	// Verify third satellite (not used)
	sat = report.Satellites[2]
	if sat.PRN != 7 || sat.Used {
		t.Errorf("Third satellite mismatch: %+v", sat)
	}
}

func TestGetSatelliteReport_ReturnsCopy(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	skyReport := SKYReport{
		Class: "SKY",
		HDOP:  1.0,
		PDOP:  2.0,
		NSat:  2,
		USat:  1,
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 10, El: 30.0, Az: 90.0, Ss: 25.0, Used: true},
		},
	}

	gps.updateSatelliteInfo(skyReport)

	// Get a copy and mutate it
	report := gps.GetSatelliteReport()
	report.Satellites[0].PRN = 999
	report.PDOP = 99.9

	// Verify the copy reflects our mutations
	if report.PDOP != 99.9 {
		t.Errorf("Copy PDOP should be 99.9, got %f", report.PDOP)
	}

	// Verify internal state was not affected
	internal := gps.GetSatelliteReport()
	if internal.Satellites[0].PRN != 10 {
		t.Errorf("Internal satellite PRN was mutated: got %d", internal.Satellites[0].PRN)
	}

	if internal.PDOP != 2.0 {
		t.Errorf("Internal PDOP was mutated: got %f", internal.PDOP)
	}
}

func TestGetSatelliteReport_EmptySatelliteList(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	skyReport := SKYReport{
		Class: "SKY",
		HDOP:  1.5,
		USat:  0,
		NSat:  0,
	}

	gps.updateSatelliteInfo(skyReport)

	report := gps.GetSatelliteReport()
	if len(report.Satellites) != 0 {
		t.Errorf("Expected empty satellites, got %d", len(report.Satellites))
	}

	if report.HDOP != 1.5 {
		t.Errorf("Expected HDOP 1.5, got %f", report.HDOP)
	}
}

// TestUpdateSatelliteInfo_TimestampOnlyOnConstellation guards the contract
// that SatelliteReport.Timestamp advances only when a SKY message carries a
// satellites array. DOP-only updates must leave the prior timestamp in
// place so the UI can distinguish "fresh constellation data" from "the
// gpsd link is alive but not reporting sky-in-view."
func TestUpdateSatelliteInfo_TimestampOnlyOnConstellation(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	if !gps.GetSatelliteReport().Timestamp.IsZero() {
		t.Fatalf("Expected zero timestamp on fresh service")
	}

	full := SKYReport{
		Class: "SKY",
		Time:  "2026-04-24T22:03:54Z",
		HDOP:  1.08,
		PDOP:  1.98,
		NSat:  12,
		USat:  8,
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 2, El: 45.0, Az: 120.0, Ss: 38.0, Used: true},
		},
	}
	gps.updateSatelliteInfo(full)

	first := gps.GetSatelliteReport().Timestamp
	expected, _ := time.Parse(time.RFC3339, "2026-04-24T22:03:54Z")
	if !first.Equal(expected) {
		t.Errorf("Expected timestamp %v from sky.Time, got %v", expected, first)
	}

	// DOP-only update must not advance the timestamp.
	partial := SKYReport{
		Class: "SKY",
		Time:  "2026-04-24T22:04:00Z",
		HDOP:  1.10,
		PDOP:  2.00,
	}
	gps.updateSatelliteInfo(partial)

	if got := gps.GetSatelliteReport().Timestamp; !got.Equal(first) {
		t.Errorf("Expected timestamp preserved at %v after DOP-only update, got %v", first, got)
	}

	// A second full SKY advances it.
	full.Time = "2026-04-24T22:04:05Z"
	gps.updateSatelliteInfo(full)

	advanced, _ := time.Parse(time.RFC3339, "2026-04-24T22:04:05Z")
	if got := gps.GetSatelliteReport().Timestamp; !got.Equal(advanced) {
		t.Errorf("Expected timestamp advanced to %v after second full SKY, got %v", advanced, got)
	}
}

// TestUpdateSatelliteInfo_TimestampFallsBackToNow covers the case where
// gpsd emits a SKY without a parseable Time field. The cache must still
// stamp something so the UI can show freshness.
func TestUpdateSatelliteInfo_TimestampFallsBackToNow(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	before := time.Now()
	gps.updateSatelliteInfo(SKYReport{
		Class: "SKY",
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 1, El: 30.0, Az: 90.0, Ss: 25.0, Used: true},
		},
	})
	after := time.Now()

	ts := gps.GetSatelliteReport().Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("Expected timestamp between %v and %v, got %v", before, after, ts)
	}
}

// TestUpdateSatelliteInfo_PartialSKYPreservesConstellation guards the merge
// behaviour of updateSatelliteInfo: a SKY report that carries only DOP values
// (as gpsd emits when the receiver is feeding $GSA but not $GSV) must not wipe
// the cached satellite list or counters from the previous full SKY. This
// regresses the bug where GpsStatus.jsx rendered an empty satelliteStatus
// alongside a populated position.pdop.
func TestUpdateSatelliteInfo_PartialSKYPreservesConstellation(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	full := SKYReport{
		Class: "SKY",
		HDOP:  1.08,
		VDOP:  1.4,
		PDOP:  1.98,
		NSat:  12,
		USat:  8,
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 2, El: 45.0, Az: 120.0, Ss: 38.0, Used: true},
			{PRN: 5, El: 72.0, Az: 210.0, Ss: 42.0, Used: true},
			{PRN: 7, El: 15.0, Az: 330.0, Ss: 18.0, Used: false},
		},
	}
	gps.updateSatelliteInfo(full)

	partial := SKYReport{
		Class: "SKY",
		HDOP:  1.10,
		PDOP:  2.00,
	}
	gps.updateSatelliteInfo(partial)

	report := gps.GetSatelliteReport()

	if len(report.Satellites) != 3 {
		t.Fatalf("Expected 3 satellites preserved, got %d", len(report.Satellites))
	}

	if report.Satellites[0].PRN != 2 || report.Satellites[2].PRN != 7 {
		t.Errorf("Satellite list corrupted: %+v", report.Satellites)
	}

	if report.NSat != 12 {
		t.Errorf("Expected NSat preserved at 12, got %d", report.NSat)
	}

	if report.USat != 8 {
		t.Errorf("Expected USat preserved at 8, got %d", report.USat)
	}

	if report.VDOP != 1.4 {
		t.Errorf("Expected VDOP preserved at 1.4, got %f", report.VDOP)
	}

	if report.HDOP != 1.10 {
		t.Errorf("Expected HDOP updated to 1.10, got %f", report.HDOP)
	}

	if report.PDOP != 2.00 {
		t.Errorf("Expected PDOP updated to 2.00, got %f", report.PDOP)
	}

	pos := gps.GetPosition()
	if pos.SatellitesUsed != 8 {
		t.Errorf("Expected position.SatellitesUsed preserved at 8, got %d", pos.SatellitesUsed)
	}

	if pos.HDOP != 1.10 {
		t.Errorf("Expected position.HDOP updated to 1.10, got %f", pos.HDOP)
	}
}

// TestUpdateSatelliteInfo_DerivesCountsWhenSummaryMissing guards against the
// bug where the dashboard and GPS page showed an incorrect "satellites
// locked" count because gpsd emitted a populated satellites array without
// the summary uSat/nSat fields. The constellation array is authoritative;
// the cache must derive USat from `used:true` entries and NSat from the
// array length when gpsd's summary is absent or zero.
func TestUpdateSatelliteInfo_DerivesCountsWhenSummaryMissing(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	sky := SKYReport{
		Class: "SKY",
		HDOP:  1.0,
		PDOP:  2.0,
		// uSat and nSat intentionally absent.
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 2, El: 45.0, Az: 120.0, Ss: 38.0, Used: true},
			{PRN: 5, El: 72.0, Az: 210.0, Ss: 42.0, Used: true},
			{PRN: 7, El: 15.0, Az: 330.0, Ss: 18.0, Used: false},
			{PRN: 9, El: 60.0, Az: 200.0, Ss: 35.0, Used: true},
		},
	}
	gps.updateSatelliteInfo(sky)

	report := gps.GetSatelliteReport()
	if report.NSat != 4 {
		t.Errorf("Expected NSat derived as 4 from array length, got %d", report.NSat)
	}

	if report.USat != 3 {
		t.Errorf("Expected USat derived as 3 from used:true entries, got %d", report.USat)
	}

	pos := gps.GetPosition()
	if pos.SatellitesUsed != 3 {
		t.Errorf("Expected position.SatellitesUsed mirrored as 3, got %d", pos.SatellitesUsed)
	}
}

// TestUpdateSatelliteInfo_SummaryWinsWhenLargerThanArray verifies that
// gpsd's summary is preferred when it reports more satellites than the
// array carries (e.g. the receiver is tracking PRNs that the SKY message
// truncated). The array's count is treated as a lower bound.
func TestUpdateSatelliteInfo_SummaryWinsWhenLargerThanArray(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	sky := SKYReport{
		Class: "SKY",
		NSat:  18,
		USat:  11,
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 2, El: 45.0, Az: 120.0, Ss: 38.0, Used: true},
			{PRN: 5, El: 72.0, Az: 210.0, Ss: 42.0, Used: true},
		},
	}
	gps.updateSatelliteInfo(sky)

	report := gps.GetSatelliteReport()
	if report.NSat != 18 {
		t.Errorf("Expected NSat from summary (18), got %d", report.NSat)
	}

	if report.USat != 11 {
		t.Errorf("Expected USat from summary (11), got %d", report.USat)
	}
}

// TestUpdateSatelliteInfo_LossOfLockClearsUSat covers the loss-of-lock
// scenario: when a constellation-bearing SKY arrives with all entries
// flagged used:false, the cached USat must drop to 0 rather than holding
// a stale value from the prior full SKY.
func TestUpdateSatelliteInfo_LossOfLockClearsUSat(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	good := SKYReport{
		Class: "SKY",
		USat:  8,
		NSat:  12,
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 2, Used: true},
			{PRN: 5, Used: true},
		},
	}
	gps.updateSatelliteInfo(good)

	if gps.GetSatelliteReport().USat != 8 {
		t.Fatalf("Expected USat 8 after good SKY, got %d", gps.GetSatelliteReport().USat)
	}

	lost := SKYReport{
		Class: "SKY",
		// Receiver no longer treats any tracked sat as part of the
		// solution. uSat is absent and the array shows used:false.
		Satellites: []struct {
			PRN  int     `json:"PRN"`
			El   float64 `json:"el"`
			Az   float64 `json:"az"`
			Ss   float64 `json:"ss"`
			Used bool    `json:"used"`
		}{
			{PRN: 2, Used: false},
			{PRN: 5, Used: false},
		},
	}
	gps.updateSatelliteInfo(lost)

	report := gps.GetSatelliteReport()
	if report.USat != 0 {
		t.Errorf("Expected USat 0 after loss of lock, got %d", report.USat)
	}

	if report.NSat != 2 {
		t.Errorf("Expected NSat 2 derived from array, got %d", report.NSat)
	}
}

func TestGetSatelliteReport_ConcurrentAccess(t *testing.T) {
	gps := &GPSService{Log: zerolog.Nop()}

	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := range 100 {
			sky := SKYReport{
				Class: "SKY",
				HDOP:  float64(i) * 0.1,
				USat:  i % 15,
				NSat:  i%15 + 5,
				Satellites: []struct {
					PRN  int     `json:"PRN"`
					El   float64 `json:"el"`
					Az   float64 `json:"az"`
					Ss   float64 `json:"ss"`
					Used bool    `json:"used"`
				}{
					{PRN: i, El: 45.0, Az: 180.0, Ss: 30.0, Used: true},
				},
			}
			gps.updateSatelliteInfo(sky)
		}
	}()

	// Reader goroutines
	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 100 {
				report := gps.GetSatelliteReport()
				// Access fields to ensure no data race
				_ = report.PDOP

				_ = report.USat
				if len(report.Satellites) > 0 {
					_ = report.Satellites[0].PRN
				}
			}
		}()
	}

	wg.Wait()
}

func TestUpdateSatelliteInfo_NegativeValues(t *testing.T) {
	log := zerolog.Nop()
	gps := &GPSService{
		Log: log,
		mu:  sync.RWMutex{},
	}

	// Set initial values
	gps.position = PositionReport{
		Valid:          true,
		SatellitesUsed: 8,
		HDOP:           1.5,
	}

	// Create a SKY report with negative values (invalid)
	skyReport := SKYReport{
		Class: "SKY",
		HDOP:  -1.0, // Negative HDOP should not update (condition is > 0)
		USat:  -3,   // Negative satellite count should not update (condition is > 0)
	}

	gps.updateSatelliteInfo(skyReport)

	pos := gps.GetPosition()
	if pos.SatellitesUsed != 8 {
		t.Errorf("Expected satellites to remain 8, got %d", pos.SatellitesUsed)
	}

	if pos.HDOP != 1.5 {
		t.Errorf("Expected HDOP to remain 1.5, got %f", pos.HDOP)
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
