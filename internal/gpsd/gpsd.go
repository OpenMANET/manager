package gpsd

import (
	"net"
	"sync"

	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
)

const (
	DefaultTAKGPSPort string = "2947"
)

type GPSService struct {
	Log            zerolog.Logger
	PositionReport *TPVReport
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
			g.PositionReport = tpv
			g.mu.Unlock()

			log.Debug().
				Float64("lat", tpv.Lat).
				Float64("lon", tpv.Lon).
				Float64("alt", tpv.Alt).
				Uint8("mode", uint8(tpv.Mode)).
				Msg("GPS position updated")
		}
	})

	// Example: Subscribe to NMEA GPGGA sentences
	session.Subscribe("GPGGA", func(r interface{}) {
		v := r.(string)

		g.sendLocationtoEUDs(v)
	})

	// Start watching for NMEA messages in the background
	session.Run(formatNMEA)

	log.Info().Str("address", address).Msg("GPS service started")

	return g, nil
}

// GetPositionReport returns the current GPS position report.
// This is safe to call from multiple goroutines.
func (g *GPSService) GetPositionReport() *TPVReport {
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

// sendLocationtoEUDs sends the current GPS location to any connected EUD clients.
// The eud devices are determined by the current dhcp leases.
func (g *GPSService) sendLocationtoEUDs(nmeaString string) {
	leases, err := network.GetCurrentDHCPLeases()
	if err != nil {
		g.Log.Error().Err(err).Msg("Error getting DHCP leases for EUD location update")
		return
	}

	// TODO: If no leases, create a CoT message and send to a predefined multicast address

	// loop through leases.DHCPleases and send location to each EUD
	for _, lease := range leases.DHCPLeases {
		// For each lease, send the current GPS location
		// We send this as a UDP packet to the EUD's IP address on a the DefaultTAKGPSPort
		addr := net.JoinHostPort(lease.IPAddr, DefaultTAKGPSPort)
		conn, err := net.Dial("udp", addr)
		if err != nil {
			g.Log.Error().Err(err).Str("ip", lease.IPAddr).Msg("Failed to connect to EUD")
			continue
		}

		_, err = conn.Write([]byte(nmeaString))
		if err != nil {
			g.Log.Error().Err(err).Str("ip", lease.IPAddr).Msg("Failed to send GPS data to EUD")
		}
		conn.Close()
	}
}
