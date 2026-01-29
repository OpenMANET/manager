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
	"encoding/xml"
	"fmt"
	"math"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/coreywagehoft/go-tak/pkg/cot"
	"github.com/coreywagehoft/go-tak/pkg/cotproto"
	"github.com/mdlayher/arp"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/util/board"
	"github.com/rs/zerolog"
	"golang.org/x/net/ipv4"
)

const (
	DefaultTAKGPSPort string = "4349"
	DefaultAddress    string = "localhost:2947"
	ATAKSAAddress     string = "239.2.3.1:6969" // ATAK Situational Awareness multicast address
	// atakMulticastTTL is the Time-To-Live value for CoT multicast packets sent to ATAK SA address
	atakMulticastTTL int = 64
	// defaultSelfMarkerType is the CoT type for self markers
	// defaultSelfMarkerType string = "a-f-G-U-C" // SELF MARKER
	// radioUnitType is the CoT type for a ground radio unit
	radioUnitType string = "G-U-U-S-R" // Gnd/RADIO UNIT;RADIO UNIT
	// defaultStaleDuration is the default duration before a CoT message is considered stale
	defaultStaleDuration time.Duration = 10 * time.Minute
	maxReconnectAttempts int           = 3
	// cotMulticastRateLimit is the minimum interval between CoT multicast sends to avoid flooding
	cotMulticastRateLimit time.Duration = 30 * time.Second // Minimum interval between CoT multicast sends
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

type GPSService struct {
	Log               zerolog.Logger
	lastMulticastTime time.Time // Last time a gps message was sent to multicast
	conn              net.Conn
	ctx               context.Context
	cancel            context.CancelFunc
	address           string
	position          PositionReport
	reconnectDelay    time.Duration
	reconnectAttempts int
	mu                sync.RWMutex
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
				g.mu.Lock()
				g.reconnectAttempts++
				attempt := g.reconnectAttempts
				g.mu.Unlock()

				g.Log.Error().Err(err).Int("attempt", attempt).Msg("Failed to connect to GPSD")

				if attempt >= maxReconnectAttempts {
					g.Log.Error().Msg("Maximum reconnection attempts reached, giving up")
					return
				}

				time.Sleep(g.reconnectDelay)
				continue
			}

			// Reset reconnection attempts on successful connection
			g.mu.Lock()
			g.reconnectAttempts = 0
			g.mu.Unlock()

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
	// Try to determine message type by checking class field
	var baseMsg struct {
		Class string `json:"class"`
	}
	err := json.Unmarshal([]byte(message), &baseMsg)
	if err != nil {
		return
	}

	switch baseMsg.Class {
	case "TPV":
		var report TPVReport
		err := json.Unmarshal([]byte(message), &report)
		if err != nil {
			return
		}
		g.updatePosition(report)

	case "SKY":
		var skyReport SKYReport
		err := json.Unmarshal([]byte(message), &skyReport)
		if err != nil {
			return
		}
		g.updateSatelliteInfo(skyReport)
	}
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
			// SatellitesUsed and HDOP are updated by SKY reports
		}

		// Send location to EUDs in a goroutine to avoid blocking
		go g.SendLocationtoEUDs()
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

	// Quality indicator based on GPS fix mode
	// 0 = Invalid, 1 = GPS fix (SPS), 2 = DGPS fix, 3 = PPS fix, etc.
	// GPSD Mode: 0/1 = no fix, 2 = 2D fix, 3 = 3D fix
	quality := "0"
	if pos.Mode >= 2 {
		quality = "1" // GPS fix (SPS)
	}

	// Number of satellites - only include if we have real data
	numSat := ""
	if pos.SatellitesUsed > 0 {
		numSat = fmt.Sprintf("%02d", pos.SatellitesUsed)
	}

	// Horizontal dilution of precision - only include if we have real data
	hdop := ""
	if pos.HDOP > 0 {
		hdop = fmt.Sprintf("%.1f", pos.HDOP)
	}

	// Altitude in meters
	altStr := fmt.Sprintf("%.1f", pos.Altitude)
	altUnit := "M"

	// Height of geoid (WGS84) above WGS84 ellipsoid - only include if we have real data
	geoidHeight := ""
	if pos.GeoidSeparation != 0 {
		geoidHeight = fmt.Sprintf("%.1f", pos.GeoidSeparation)
	}
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
	// Rate limit: only send once every 30 seconds to avoid flooding the network
	g.mu.Lock()
	if time.Since(g.lastMulticastTime) < cotMulticastRateLimit {
		g.mu.Unlock()
		return // Rate limited, exit early
	}
	g.lastMulticastTime = time.Now()
	g.mu.Unlock()

	// Check if we have a valid GPS position
	if !g.IsValid() {
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

	// Track if we sent to any active devices
	activeDeviceCount := 0

	// loop through leases.DHCPleases and send location to each EUD
	for _, lease := range leases.DHCPLeases {
		// Send an ARP request to verify the EUD is online
		if !g.checkDeviceActive(lease.IPAddr) {
			g.Log.Debug().Str("ip", lease.IPAddr).Msg("Device not responding to ARP, skipping")
			continue
		}

		if err := g.sendNMEAasExternalGPS(lease.IPAddr); err != nil {
			g.Log.Error().Err(err).Str("ip", lease.IPAddr).Msg("Failed to send NMEA to EUD")
			continue
		}

		// Send CoT message as External GPS to the EUD
		if err := g.sendCoTTAsExternalGPS(lease.IPAddr); err != nil {
			g.Log.Error().Err(err).Str("ip", lease.IPAddr).Msg("Failed to send CoT to EUD")
			continue
		}
		activeDeviceCount++
	}

	// If no active devices were found, send to multicast as fallback
	if activeDeviceCount == 0 {
		g.Log.Debug().Msg("No active devices found, sending CoT to ATAK SA multicast address")
		if err := g.sendCoTToMulticast(); err != nil {
			g.Log.Error().Err(err).Msg("Failed to send CoT to multicast")
		}
	}
}

// checkDeviceActive performs an ARP request to check if a device is active at the given IP address
func (g *GPSService) checkDeviceActive(ipAddr string) bool {
	// Parse the IP address as netip.Addr
	ipAddrParsed, err := netip.ParseAddr(ipAddr)
	if err != nil {
		g.Log.Debug().Str("ip", ipAddr).Msg("Invalid IP address for ARP check")
		return false
	}

	// Get all network interfaces
	ifaces, err := net.Interfaces()
	if err != nil {
		g.Log.Debug().Err(err).Msg("Failed to get network interfaces for ARP check")
		return false
	}

	// Try to find an interface on the same subnet as the target IP
	for _, iface := range ifaces {
		// Skip down or loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Get addresses for this interface
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		// Check if any address is on the same subnet
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			// Convert to check if target IP is in this subnet
			ip := net.ParseIP(ipAddr)
			if ip == nil {
				continue
			}

			// Check if target IP is in this subnet
			if ipNet.Contains(ip) {
				// Create ARP client for this interface
				client, err := arp.Dial(&iface)
				if err != nil {
					g.Log.Debug().Err(err).Str("interface", iface.Name).Msg("Failed to create ARP client")
					continue
				}
				defer client.Close()

				// Set a short timeout for ARP request
				if err := client.SetDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
					g.Log.Debug().Err(err).Msg("Failed to set ARP deadline")
					client.Close()
					continue
				}

				// Perform ARP request using netip.Addr
				_, err = client.Resolve(ipAddrParsed)
				client.Close()

				if err == nil {
					g.Log.Debug().Str("ip", ipAddr).Msg("Device is active (ARP response received)")
					return true
				}

				g.Log.Debug().Err(err).Str("ip", ipAddr).Msg("Device did not respond to ARP request")
				return false
			}
		}
	}

	// If we get here, we couldn't find a suitable interface
	// Return true to allow the send attempt (conservative approach)
	return true
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

	// Calculate Height Above Ellipsoid (HAE)
	// HAE = MSL altitude + Geoid Separation
	hae := pos.Altitude
	if pos.GeoidSeparation != 0 {
		hae = pos.Altitude + pos.GeoidSeparation
	}

	cotMsg.CotEvent = &cotproto.CotEvent{
		Lat: pos.Latitude,
		Lon: pos.Longitude,
		Hae: hae,
		Ce:  pos.HDOP,
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
			PrecisionLocation: &cotproto.PrecisionLocation{
				Geopointsrc: "GPS",
				Altsrc:      "GPS",
			},
		},
	}

	cotEvent := cot.ProtoToEvent(cotMsg)

	// Marshal to XML
	xmlData, err := xml.Marshal(cotEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal CoT XML: %w", err)
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

	// Set multicast TTL to 64
	pconn := ipv4.NewPacketConn(conn)
	if err := pconn.SetMulticastTTL(atakMulticastTTL); err != nil {
		g.Log.Warn().Err(err).Msg("Failed to set multicast TTL")
	}

	_, err = conn.Write(xmlData)
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

// sendCoTAsExternalGPS creates and sends an ATAK CoT message to the EUD.
func (g *GPSService) sendCoTTAsExternalGPS(iPAddr string) error {
	pos := g.GetPosition()
	if !pos.Valid {
		return fmt.Errorf("no valid GPS position")
	}

	// Calculate Height Above Ellipsoid (HAE)
	// HAE = MSL altitude + Geoid Separation
	hae := pos.Altitude
	if pos.GeoidSeparation != 0 {
		hae = pos.Altitude + pos.GeoidSeparation
	}

	event := &cotproto.CotEvent{
		Uid:       "External-GPS",
		Type:      "a-f-G-E-S",
		SendTime:  cot.TimeToMillis(time.Now()),
		StartTime: cot.TimeToMillis(time.Now()),
		StaleTime: cot.TimeToMillis(time.Now().Add(defaultStaleDuration)),
		How:       cot.HowDefault,
		Lat:       pos.Latitude,
		Lon:       pos.Longitude,
		Hae:       hae,
		Le:        0,
		Ce:        pos.HDOP,
		Detail: &cotproto.Detail{
			Track: &cotproto.Track{
				Speed:  pos.Speed,
				Course: pos.Track,
			},
			PrecisionLocation: &cotproto.PrecisionLocation{
				Geopointsrc: "GPS",
				Altsrc:      "GPS",
			},
		},
	}

	cotEvent := cot.CotToEvent(event)

	// Marshal to XML
	xmlData, err := xml.Marshal(cotEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal CoT XML: %w", err)
	}

	// Send to device address
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%s", iPAddr, DefaultTAKGPSPort))
	if err != nil {
		return fmt.Errorf("failed to resolve device address: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to dial device address: %w", err)
	}
	defer conn.Close()

	_, err = conn.Write(xmlData)
	if err != nil {
		return fmt.Errorf("failed to send CoT message: %w", err)
	}

	g.Log.Debug().
		Float64("lat", pos.Latitude).
		Float64("lon", pos.Longitude).
		Float64("alt", pos.Altitude).
		Str("address", iPAddr).
		Msg("Sent CoT message to ATAK device")

	return nil
}

// sendNMEAasExternalGPS creates and sends a udp NMEA message to the EUD.
func (g *GPSService) sendNMEAasExternalGPS(iPAddr string) error {
	pos := g.GetPosition()
	if !pos.Valid {
		return fmt.Errorf("no valid GPS position")
	}

	// Send to device address
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%s", iPAddr, DefaultTAKGPSPort))
	if err != nil {
		return fmt.Errorf("failed to resolve device address: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to dial device address: %w", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte(g.ToNMEA()))
	if err != nil {
		return fmt.Errorf("failed to send CoT message: %w", err)
	}

	g.Log.Debug().
		Float64("lat", pos.Latitude).
		Float64("lon", pos.Longitude).
		Float64("alt", pos.Altitude).
		Str("address", iPAddr).
		Msg("Sent NMEA message to ATAK device")

	return nil
}
