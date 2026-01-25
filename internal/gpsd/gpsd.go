// Package gpsd provides a client for connecting to and reading GPS data from a GPSD server.
// It automatically maintains a connection to GPSD, reads TPV (Time-Position-Velocity) reports,
// and provides thread-safe access to the current GPS position data.
//
// Example usage:
//
//	gps, err := gpsd.NewGPSService(log)
//	if err != nil {
//	    return err
//	}
//	defer gps.Close()
//
//	// Get current position
//	pos := gps.GetPosition()
//	if pos.Valid {
//	    fmt.Printf("Lat: %f, Lon: %f\n", pos.Latitude, pos.Longitude)
//	}
//
//	// Get NMEA formatted output
//	nmea := gps.ToNMEA()
//	if nmea != "" {
//	    fmt.Println(nmea)
//	}
package gpsd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"sync"
	"time"

	"github.com/coreywagehoft/go-tak/pkg/cot"
	"github.com/coreywagehoft/go-tak/pkg/cotproto"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/util/board"
	"github.com/rs/zerolog"
)

const (
	DefaultTAKGPSPort    string = "2947"
	DefaultAddress       string = "localhost:2947"
	ATAKSAAddress        string = "239.2.3.1:6969" // ATAK Situational Awareness multicast address
	radioUnitType        string = "G-U-U-S-R"      // Gnd/RADIO UNIT;RADIO UNIT
	defaultStaleDuration        = 10 * time.Minute
)

// PositionReport holds the current GPS position data
type PositionReport struct {
	Timestamp time.Time // Time of position fix
	Latitude  float64   // Latitude in degrees
	Longitude float64   // Longitude in degrees
	Altitude  float64   // Altitude in meters above sea level
	Speed     float64   // Speed over ground in m/s
	Track     float64   // Course over ground in degrees
	Climb     float64   // Climb/sink rate in m/s
	Valid     bool      // Whether the position data is valid
	Mode      int       // GPS fix mode (0=no fix, 1=no fix, 2=2D, 3=3D)
}

// TPVReport represents a Time-Position-Velocity report from GPSD
type TPVReport struct {
	Class  string  `json:"class"`
	Device string  `json:"device,omitempty"`
	Mode   int     `json:"mode"`
	Time   string  `json:"time,omitempty"`
	Lat    float64 `json:"lat,omitempty"`
	Lon    float64 `json:"lon,omitempty"`
	Alt    float64 `json:"alt,omitempty"`
	Track  float64 `json:"track,omitempty"`
	Speed  float64 `json:"speed,omitempty"`
	Climb  float64 `json:"climb,omitempty"`
}

type GPSService struct {
	Log            zerolog.Logger
	mu             sync.RWMutex
	position       PositionReport
	conn           net.Conn
	ctx            context.Context
	cancel         context.CancelFunc
	address        string
	reconnectDelay time.Duration
}

// NewGPSService creates a new GPS service that connects to GPSD and monitors TPV reports.
// It connects to GPSD at the default address (localhost:2947) and watches for NMEA messages.
// The PositionReport is automatically updated when new TPV data is received.
func NewGPSService(log zerolog.Logger) (*GPSService, error) {
	return NewGPSServiceWithAddress(log, DefaultAddress)
}

// NewGPSServiceWithAddress creates a new GPS service with a custom GPSD address.
// It creates two separate sessions: one for JSON/TPV reports and one for NMEA sentences.
func NewGPSServiceWithAddress(log zerolog.Logger, address string) (*GPSService, error) {
	ctx, cancel := context.WithCancel(context.Background())

	g := &GPSService{
		Log:            log,
		address:        address,
		ctx:            ctx,
		cancel:         cancel,
		reconnectDelay: 5 * time.Second,
	}

	// Start the connection handler in a goroutine
	go g.connectionHandler()

	return g, nil
}

// Close stops the GPS service and closes the connection to GPSD
func (g *GPSService) Close() error {
	if g.cancel != nil {
		g.cancel()
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.conn != nil {
		return g.conn.Close()
	}
	return nil
}

// connectionHandler manages the connection to GPSD with automatic reconnection
func (g *GPSService) connectionHandler() {
	for {
		select {
		case <-g.ctx.Done():
			return
		default:
			err := g.connect()
			if err != nil {
				g.Log.Error().Err(err).Msg("Failed to connect to GPSD")
				time.Sleep(g.reconnectDelay)
				continue
			}

			// Start reading data
			g.readGPSD()

			// If we get here, connection was lost
			g.Log.Warn().Msg("Connection to GPSD lost, reconnecting...")
			time.Sleep(g.reconnectDelay)
		}
	}
}

// connect establishes a connection to GPSD and sends the watch command
func (g *GPSService) connect() error {
	conn, err := net.Dial("tcp", g.address)
	if err != nil {
		return fmt.Errorf("failed to dial GPSD: %w", err)
	}

	g.mu.Lock()
	g.conn = conn
	g.mu.Unlock()

	// Enable watching for updates with JSON output
	watchCmd := "?WATCH={\"enable\":true,\"json\":true}\n"
	_, err = conn.Write([]byte(watchCmd))
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to send watch command: %w", err)
	}

	g.Log.Info().Str("address", g.address).Msg("Connected to GPSD")
	return nil
}

// readGPSD reads and processes data from GPSD
func (g *GPSService) readGPSD() {
	g.mu.RLock()
	conn := g.conn
	g.mu.RUnlock()

	if conn == nil {
		return
	}

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		select {
		case <-g.ctx.Done():
			return
		default:
			line := scanner.Text()
			g.processGPSDMessage(line)
		}
	}

	if err := scanner.Err(); err != nil {
		g.Log.Error().Err(err).Msg("Error reading from GPSD")
	}
}

// processGPSDMessage parses and processes a JSON message from GPSD
func (g *GPSService) processGPSDMessage(message string) {
	var report TPVReport
	err := json.Unmarshal([]byte(message), &report)
	if err != nil {
		// Not all messages are TPV reports, so we can ignore parse errors
		return
	}

	// Only process TPV reports with valid data
	if report.Class != "TPV" {
		return
	}

	g.updatePosition(report)
}

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

		g.position = PositionReport{
			Timestamp: timestamp,
			Latitude:  tpv.Lat,
			Longitude: tpv.Lon,
			Altitude:  tpv.Alt,
			Speed:     tpv.Speed,
			Track:     tpv.Track,
			Climb:     tpv.Climb,
			Valid:     true,
			Mode:      tpv.Mode,
		}

		g.Log.Debug().
			Float64("lat", tpv.Lat).
			Float64("lon", tpv.Lon).
			Float64("alt", tpv.Alt).
			Msg("Position updated")

		// Send location to EUDs in a goroutine to avoid blocking
		go g.SendLocationtoEUDs()
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

// ToNMEA converts the current position to NMEA GGA format
// GGA - Global Positioning System Fix Data
func (g *GPSService) ToNMEA() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.position.Valid {
		return ""
	}

	return formatGGA(g.position)
}

// formatGGA formats a position report as an NMEA GGA sentence
func formatGGA(pos PositionReport) string {
	// Calculate time in HHMMSS.ss format
	timeStr := pos.Timestamp.UTC().Format("150405.00")

	// Convert latitude to NMEA format (DDMM.MMMM)
	latDeg := math.Abs(pos.Latitude)
	latDegInt := int(latDeg)
	latMin := (latDeg - float64(latDegInt)) * 60.0
	latStr := fmt.Sprintf("%02d%08.5f", latDegInt, latMin)
	latHem := "N"
	if pos.Latitude < 0 {
		latHem = "S"
	}

	// Convert longitude to NMEA format (DDDMM.MMMM)
	lonDeg := math.Abs(pos.Longitude)
	lonDegInt := int(lonDeg)
	lonMin := (lonDeg - float64(lonDegInt)) * 60.0
	lonStr := fmt.Sprintf("%03d%08.5f", lonDegInt, lonMin)
	lonHem := "E"
	if pos.Longitude < 0 {
		lonHem = "W"
	}

	// Quality indicator (1 = GPS fix)
	quality := "1"

	// Number of satellites (we don't have this, use a default)
	numSat := "08"

	// Horizontal dilution of precision (we don't have this, use a default)
	hdop := "1.0"

	// Altitude in meters
	altStr := fmt.Sprintf("%.1f", pos.Altitude)
	altUnit := "M"

	// Height of geoid (WGS84) above WGS84 ellipsoid (we don't have this, use 0)
	geoidHeight := "0.0"
	geoidUnit := "M"

	// Time since last DGPS update (empty if not using DGPS)
	dgpsAge := ""

	// DGPS station ID (empty if not using DGPS)
	dgpsID := ""

	// Construct the NMEA sentence (without checksum initially)
	sentence := fmt.Sprintf("GPGGA,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s",
		timeStr, latStr, latHem, lonStr, lonHem, quality, numSat, hdop,
		altStr, altUnit, geoidHeight, geoidUnit, dgpsAge, dgpsID)

	// Calculate checksum
	checksum := calculateNMEAChecksum(sentence)

	// Return complete NMEA sentence with $ prefix and checksum
	return fmt.Sprintf("$%s*%02X", sentence, checksum)
}

// calculateNMEAChecksum calculates the XOR checksum for an NMEA sentence
func calculateNMEAChecksum(sentence string) byte {
	var checksum byte = 0
	for i := 0; i < len(sentence); i++ {
		checksum ^= sentence[i]
	}
	return checksum
}

// SendLocationtoEUDs sends the current GPS location to any connected EUD clients.
// The eud devices are determined by the current dhcp leases.
// If no DHCP leases are found, it sends a CoT message to the ATAK SA multicast address.
func (g *GPSService) SendLocationtoEUDs() {
	nmeaString := g.ToNMEA()
	if nmeaString == "" {
		g.Log.Warn().Msg("No valid GPS position to send to EUDs")
		return
	}

	leases, err := network.GetCurrentDHCPLeases()
	if err != nil {
		g.Log.Error().Err(err).Msg("Error getting DHCP leases for EUD location update")
		return
	}

	// If no leases, create a CoT message and send to the ATAK SA multicast address
	if len(leases.DHCPLeases) == 0 {
		g.Log.Debug().Msg("No DHCP leases found, sending CoT to ATAK SA multicast address")
		if err := g.sendCoTToMulticast(); err != nil {
			g.Log.Error().Err(err).Msg("Failed to send CoT to multicast")
		}
		return
	}

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

// sendCoTToMulticast creates and sends an ATAK CoT message to the standard multicast address
func (g *GPSService) sendCoTToMulticast() error {
	pos := g.GetPosition()
	if !pos.Valid {
		return fmt.Errorf("no valid GPS position")
	}

	deviceInfo, err := board.NewBoardConfigInfo()
	if err != nil {
		g.Log.Warn().Err(err).Msg("Failed to get board config info for CoT message")
	}

	// Get hostname for callsign
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "openmanet-node"
	}

	// Get platform name, handle nil deviceInfo
	platformName := "OpenMANET"
	if deviceInfo != nil {
		modelName := deviceInfo.Model.GetName()
		if modelName != "" {
			platformName = modelName
		}
	}

	// Create CoT Message
	cotMsg := cot.BasicMsg(radioUnitType, hostname, defaultStaleDuration)

	cotMsg.CotEvent = &cotproto.CotEvent{
		Lat: pos.Latitude,
		Lon: pos.Longitude,
		Hae: pos.Altitude,
		Detail: &cotproto.Detail{
			Contact: &cotproto.Contact{
				Callsign: fmt.Sprintf("%s-manet", hostname),
			},
			Group: &cotproto.Group{
				Name: "MANET",
				Role: "Radio Unit",
			},
			Takv: &cotproto.Takv{
				Os:       "OpenMANET",
				Device:   hostname,
				Platform: platformName,
			},
			Track: &cotproto.Track{
				Speed:  pos.Speed,
				Course: pos.Track,
			},
		},
	}

	// Convert to protobuf packet
	protoData, err := cot.MakeProtoPacket(cotMsg)
	if err != nil {
		return fmt.Errorf("failed to create CoT protobuf packet: %w", err)
	}

	// Send to multicast address
	addr, err := net.ResolveUDPAddr("udp", ATAKSAAddress)
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to dial multicast: %w", err)
	}
	defer conn.Close()

	_, err = conn.Write(protoData)
	if err != nil {
		return fmt.Errorf("failed to send CoT message: %w", err)
	}

	g.Log.Debug().
		Str("callsign", hostname).
		Float64("lat", pos.Latitude).
		Float64("lon", pos.Longitude).
		Float64("alt", pos.Altitude).
		Str("address", ATAKSAAddress).
		Msg("Sent CoT message to ATAK SA multicast")

	return nil
}
