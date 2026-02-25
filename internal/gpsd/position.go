package gpsd

import (
	"time"
)

// updatePosition updates the internal position report from a TPV message
func (g *GPSService) updatePosition(tpv TPVReport) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Mode 2 = 2D fix, Mode 3 = 3D fix
	valid := tpv.Mode >= 2

	if valid {
		var timestamp time.Time
		if tpv.Time != "" {
			timestamp, _ = time.Parse(time.RFC3339, tpv.Time)
		} else {
			timestamp = time.Now()
		}

		// Preserve satellite and HDOP data from previous SKY reports
		prevSatellitesUsed := g.position.SatellitesUsed
		prevHDOP := g.position.HDOP

		g.position = PositionReport{
			Timestamp:       timestamp,
			Latitude:        tpv.Lat,
			Longitude:       tpv.Lon,
			Altitude:        tpv.Alt,
			Speed:           tpv.Speed,
			Track:           tpv.Track,
			Climb:           tpv.Climb,
			Valid:           true,
			Mode:            tpv.Mode,
			GeoidSeparation: tpv.GeoidSep,
			EPH:             tpv.EPH,
			EPX:             tpv.EPX,
			EPY:             tpv.EPY,
			EPV:             tpv.EPV,
			DGPSAge:         tpv.DGPSAge,
			DGPSStation:     tpv.DGPSSta,
			SatellitesUsed:  prevSatellitesUsed, // Preserve from SKY reports
			HDOP:            prevHDOP,           // Preserve from SKY reports
		}

		// Send location to ATAK SA if no
		// devices are active in a goroutine to avoid blocking
		go g.SendIfRequiredAsCoT()
	}
}

// updateSatelliteInfo updates the satellite and precision information from a SKY report
func (g *GPSService) updateSatelliteInfo(sky SKYReport) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Only update if we have valid satellite data
	if sky.USat > 0 {
		g.position.SatellitesUsed = sky.USat
	}

	// Update HDOP if available
	if sky.HDOP > 0 {
		g.position.HDOP = sky.HDOP
	}
}

// GetPosition returns a copy of the current position report
func (g *GPSService) GetPosition() PositionReport {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position
}

// GetLatitude returns the current latitude in degrees
func (g *GPSService) GetLatitude() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.Latitude
}

// GetLongitude returns the current longitude in degrees
func (g *GPSService) GetLongitude() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.Longitude
}

// GetAltitude returns the current altitude in meters above sea level
func (g *GPSService) GetAltitude() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.Altitude
}

// GetSpeed returns the current speed over ground in m/s
func (g *GPSService) GetSpeed() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.Speed
}

// GetTrack returns the current course over ground in degrees
func (g *GPSService) GetTrack() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.Track
}

// IsValid returns whether the current position data is valid
func (g *GPSService) IsValid() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.Valid
}

// GetMode returns the GPS fix mode (0=no fix, 1=no fix, 2=2D, 3=3D)
func (g *GPSService) GetMode() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.Mode
}

// GetHorizontalAccuracy returns the estimated horizontal position error (1-sigma) in meters.
// This represents the circular uncertainty about the position (CEP).
// Returns 0 if no error estimate is available from GPSD.
func (g *GPSService) GetHorizontalAccuracy() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.EPH
}

// GetVerticalAccuracy returns the estimated altitude error (1-sigma) in meters.
// Returns 0 if no error estimate is available from GPSD.
func (g *GPSService) GetVerticalAccuracy() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.EPV
}

// GetLongitudeError returns the estimated longitude error (1-sigma) in meters.
// Returns 0 if no error estimate is available from GPSD.
func (g *GPSService) GetLongitudeError() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.EPX
}

// GetLatitudeError returns the estimated latitude error (1-sigma) in meters.
// Returns 0 if no error estimate is available from GPSD.
func (g *GPSService) GetLatitudeError() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.EPY
}

// GetDGPSAge returns the age of DGPS data in seconds.
// Returns 0 if DGPS is not being used.
func (g *GPSService) GetDGPSAge() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.DGPSAge
}

// GetDGPSStation returns the DGPS reference station ID.
// Returns 0 if DGPS is not being used.
func (g *GPSService) GetDGPSStation() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.position.DGPSStation
}
