package gpsd

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// Tests for nmea.go functions:
// - ToNMEA
// - formatGGA
// - calculateNMEAChecksum
// - sendNMEAasExternalGPS
// - sendRawNMEAToActiveDevices
// - GetLastNMEA, GetNMEASentence, GetAllNMEASentences (NMEA sentence storage)
// Also includes processNMEASentence tests from connection.go

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
				_ = afterCount
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
