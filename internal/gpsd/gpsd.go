package gpsd

import (
	"sync"

	"github.com/rs/zerolog"
)

type GPSService struct {
	Log            zerolog.Logger
	PositionReport TPVReport
	session        *Session
	mu             sync.RWMutex
}

// NewGPSService creates a new GPS service that connects to GPSD and monitors TPV reports.
// It connects to GPSD at the default address (localhost:2947) and watches for NMEA messages.
// The PositionReport is automatically updated when new TPV data is received.
func NewGPSService(log zerolog.Logger) (*GPSService, error) {
	return NewGPSServiceWithAddress(log, DefaultAddress)
}

// NewGPSServiceWithAddress creates a new GPS service with a custom GPSD address.
func NewGPSServiceWithAddress(log zerolog.Logger, address string) (*GPSService, error) {
	session, err := Dial(address)
	if err != nil {
		return nil, err
	}

	g := &GPSService{
		Log:     log,
		session: session,
	}

	// Subscribe to TPV (Time-Position-Velocity) reports
	session.Subscribe(msgClassTPV, func(report interface{}) {
		if tpv, ok := report.(*TPVReport); ok {
			g.mu.Lock()
			g.PositionReport = *tpv
			g.mu.Unlock()
			
			log.Debug().
				Float64("lat", tpv.Lat).
				Float64("lon", tpv.Lon).
				Float64("alt", tpv.Alt).
				Uint8("mode", uint8(tpv.Mode)).
				Msg("GPS position updated")
		}
	})

	// Start watching for NMEA messages in the background
	session.Run(formatJSON)

	log.Info().Str("address", address).Msg("GPS service started")

	return g, nil
}

// GetPositionReport returns the current GPS position report.
// This is safe to call from multiple goroutines.
func (g *GPSService) GetPositionReport() TPVReport {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.PositionReport
}

// Close stops the GPS service and closes the connection to GPSD.
func (g *GPSService) Close() error {
	if g.session != nil {
		return g.session.Close()
	}
	return nil
}