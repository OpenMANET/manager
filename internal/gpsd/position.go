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

// updateSatelliteInfo merges satellite and precision data from a SKY report
// into the cached report. gpsd legitimately emits SKY messages that omit the
// satellites array or the nSat/uSat counters (for example when the receiver
// supplies $GSA but not $GSV), so each field is updated only when the
// incoming message actually carries it.
func (g *GPSService) updateSatelliteInfo(sky SKYReport) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if sky.USat > 0 {
		g.position.SatellitesUsed = sky.USat
	}

	if sky.HDOP > 0 {
		g.position.HDOP = sky.HDOP
	}

	if len(sky.Satellites) > 0 {
		sats := make([]SatelliteInfo, 0, len(sky.Satellites))
		for _, s := range sky.Satellites {
			sats = append(sats, SatelliteInfo{
				PRN:  s.PRN,
				El:   s.El,
				Az:   s.Az,
				Ss:   s.Ss,
				Used: s.Used,
			})
		}

		g.satellites.Satellites = sats

		// Stamp only when the message actually carries a constellation so
		// the timestamp reflects sky-in-view freshness, not DOP-only chatter.
		ts, err := time.Parse(time.RFC3339, sky.Time)
		if err != nil {
			ts = time.Now()
		}

		g.satellites.Timestamp = ts
	}

	if sky.HDOP > 0 {
		g.satellites.HDOP = sky.HDOP
	}

	if sky.VDOP > 0 {
		g.satellites.VDOP = sky.VDOP
	}

	if sky.PDOP > 0 {
		g.satellites.PDOP = sky.PDOP
	}

	if sky.NSat > 0 {
		g.satellites.NSat = sky.NSat
	}

	if sky.USat > 0 {
		g.satellites.USat = sky.USat
	}
}

// GetSatelliteReport returns a copy of the current satellite report.
func (g *GPSService) GetSatelliteReport() SatelliteReport {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Return a deep copy to avoid sharing the slice
	report := g.satellites
	if len(g.satellites.Satellites) > 0 {
		report.Satellites = make([]SatelliteInfo, len(g.satellites.Satellites))
		copy(report.Satellites, g.satellites.Satellites)
	}

	return report
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
