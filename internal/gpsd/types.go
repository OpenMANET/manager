package gpsd

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
)

// PositionReport holds the current GPS position data
type PositionReport struct {
	Timestamp       time.Time // Time of position fix
	Latitude        float64   // Latitude in degrees
	Longitude       float64   // Longitude in degrees
	Altitude        float64   // Altitude in meters above sea level
	Speed           float64   // Speed over ground in m/s
	Track           float64   // Course over ground in degrees
	Climb           float64   // Climb/sink rate in m/s
	Valid           bool      // Whether the position data is valid
	Mode            int       // GPS fix mode (0=no fix, 1=no fix, 2=2D, 3=3D)
	SatellitesUsed  int       // Number of satellites used in navigation solution
	HDOP            float64   // Horizontal dilution of precision
	GeoidSeparation float64   // Height of geoid above WGS84 ellipsoid in meters
	EPH             float64   // Estimated horizontal position error (1-sigma, meters)
	EPX             float64   // Estimated longitude error (1-sigma, meters)
	EPY             float64   // Estimated latitude error (1-sigma, meters)
	EPV             float64   // Estimated altitude error (1-sigma, meters)
	DGPSAge         float64   // Age of DGPS data in seconds (0 if not using DGPS)
	DGPSStation     int       // DGPS reference station ID (0 if not using DGPS)
}

// TPVReport represents a Time-Position-Velocity report from GPSD
type TPVReport struct {
	Class    string  `json:"class"`
	Device   string  `json:"device,omitempty"`
	Time     string  `json:"time,omitempty"`
	Mode     int     `json:"mode"`
	Lat      float64 `json:"lat,omitempty"`
	Lon      float64 `json:"lon,omitempty"`
	Alt      float64 `json:"alt,omitempty"`
	Track    float64 `json:"track,omitempty"`
	Speed    float64 `json:"speed,omitempty"`
	Climb    float64 `json:"climb,omitempty"`
	GeoidSep float64 `json:"geoidSep,omitempty"` // Geoid separation in meters
	EPH      float64 `json:"eph,omitempty"`      // Estimated horizontal position error (1-sigma, meters)
	EPX      float64 `json:"epx,omitempty"`      // Estimated longitude error (1-sigma, meters)
	EPY      float64 `json:"epy,omitempty"`      // Estimated latitude error (1-sigma, meters)
	EPV      float64 `json:"epv,omitempty"`      // Estimated altitude error (1-sigma, meters)
	DGPSAge  float64 `json:"dgpsAge,omitempty"`  // Age of DGPS data in seconds
	DGPSSta  int     `json:"dgpsSta,omitempty"`  // DGPS reference station ID
}

// SKYReport represents a satellite information report from GPSD
type SKYReport struct {
	Class      string `json:"class"`
	Device     string `json:"device,omitempty"`
	Time       string `json:"time,omitempty"`
	Satellites []struct {
		PRN  int     `json:"PRN"`
		El   float64 `json:"el"`
		Az   float64 `json:"az"`
		Ss   float64 `json:"ss"`
		Used bool    `json:"used"`
	} `json:"satellites,omitempty"`
	HDOP float64 `json:"hdop,omitempty"` // Horizontal dilution of precision
	VDOP float64 `json:"vdop,omitempty"` // Vertical dilution of precision
	PDOP float64 `json:"pdop,omitempty"` // Position dilution of precision
	NSat int     `json:"nSat,omitempty"` // Number of satellites visible
	USat int     `json:"uSat,omitempty"` // Number of satellites used in solution
}

// GPSService represents a GPS service client that connects to GPSD
type GPSService struct {
	Log               zerolog.Logger
	lastMulticastTime time.Time // Last time a gps message was sent to multicast
	conn              net.Conn
	ctx               context.Context
	Config            *config.Config
	cancel            context.CancelFunc
	nmeaSentences     map[string]string // Map of NMEA sentence types to their latest values
	address           string
	lastNMEA          string // Last raw NMEA sentence received from GPSD
	position          PositionReport
	reconnectDelay    time.Duration
	reconnectAttempts int
	mu                sync.RWMutex
}
