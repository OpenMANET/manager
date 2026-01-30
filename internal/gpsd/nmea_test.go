package gpsd

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
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
