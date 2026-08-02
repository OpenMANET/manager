package gpsd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
)

const (
	gpsdJSONWatchCommand = "?WATCH={\"enable\":true,\"json\":true}\n"
	gpsdNMEAWatchCommand = "?WATCH={\"enable\":true,\"nmea\":true}\n"
)

// NewGPSService creates a new GPS service that connects to GPSD and monitors TPV reports.
func NewGPSService(log zerolog.Logger, cfg *config.Config) (*GPSService, error) {
	return NewGPSServiceWithAddress(log, cfg, DefaultGPSDAddress)
}

// NewGPSServiceWithAddress creates a new GPS service with a custom GPSD address.
// JSON/TPV reports and raw NMEA sentences use independent gpsd sessions. Some
// gpsd versions do not emit TPV reports when JSON and NMEA are requested on the
// same WATCH session.
func NewGPSServiceWithAddress(log zerolog.Logger, cfg *config.Config, address string) (*GPSService, error) {
	done := make(chan struct{})
	cancel := context.CancelFunc(func() {
		select {
		case <-done:
		default:
			close(done)
		}
	})

	g := &GPSService{
		Log:            log,
		Config:         cfg,
		address:        address,
		done:           done,
		cancel:         cancel,
		reconnectDelay: 5 * time.Second,
	}

	// Start the connection handler in a goroutine
	go g.connectionHandler()

	if g.nmeaForwardingEnabled() {
		go g.nmeaConnectionHandler()
	}

	return g, nil
}

// Close stops the GPS service and closes the connection to GPSD
func (g *GPSService) Close() error {
	if g.cancel != nil {
		g.cancel()
	}

	g.mu.Lock()
	connections := []net.Conn{g.conn, g.nmeaConn}
	g.conn = nil
	g.nmeaConn = nil
	g.mu.Unlock()

	var firstErr error

	for _, conn := range connections {
		if conn == nil {
			continue
		}

		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// connectionHandler manages the connection to GPSD with automatic reconnection
func (g *GPSService) connectionHandler() {
	for {
		select {
		case <-g.done:
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

// nmeaConnectionHandler manages the optional raw NMEA connection separately
// from the JSON connection used for TPV/SKY and CoT generation.
func (g *GPSService) nmeaConnectionHandler() {
	for {
		select {
		case <-g.done:
			return
		default:
			err := g.connectNMEA()
			if err != nil {
				g.Log.Error().Err(err).Msg("Failed to connect to GPSD for NMEA")
				time.Sleep(g.reconnectDelay)

				continue
			}

			g.readNMEA()
			g.Log.Warn().Msg("NMEA connection to GPSD lost, reconnecting...")
			time.Sleep(g.reconnectDelay)
		}
	}
}

// connect establishes the JSON-only GPSD connection used for TPV/SKY reports.
func (g *GPSService) connect() error {
	conn, err := g.connectWithWatch(gpsdJSONWatchCommand)
	if err != nil {
		return err
	}

	g.mu.Lock()
	g.conn = conn
	g.mu.Unlock()

	g.Log.Info().Str("address", g.address).Msg("Connected to GPSD")

	return nil
}

func (g *GPSService) connectNMEA() error {
	conn, err := g.connectWithWatch(gpsdNMEAWatchCommand)
	if err != nil {
		return err
	}

	g.mu.Lock()
	g.nmeaConn = conn
	g.mu.Unlock()

	g.Log.Info().Str("address", g.address).Msg("Connected to GPSD for NMEA")

	return nil
}

func (g *GPSService) connectWithWatch(watchCommand string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", g.address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial GPSD: %w", err)
	}

	_, err = conn.Write([]byte(watchCommand))
	if err != nil {
		conn.Close()

		return nil, fmt.Errorf("failed to send watch command: %w", err)
	}

	return conn, nil
}

// readGPSD reads and processes data from GPSD
func (g *GPSService) readGPSD() {
	g.mu.RLock()
	conn := g.conn
	g.mu.RUnlock()
	g.readGPSDConnection(conn, g.processGPSDMessage)
}

// readNMEA reads raw NMEA sentences from the NMEA-only gpsd session.
func (g *GPSService) readNMEA() {
	g.mu.RLock()
	conn := g.nmeaConn
	g.mu.RUnlock()
	g.readGPSDConnection(conn, g.processNMEASentence)
}

func (g *GPSService) readGPSDConnection(conn net.Conn, process func(string)) {
	if conn == nil {
		return
	}

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		select {
		case <-g.done:
			return
		default:
			process(scanner.Text())
		}
	}

	if err := scanner.Err(); err != nil {
		g.Log.Error().Err(err).Msg("Error reading from GPSD")
	}
}

func (g *GPSService) nmeaForwardingEnabled() bool {
	return g.Config != nil && g.Config.GetGNSSSendAsNMEA()
}

// processGPSDMessage parses and processes a message from GPSD (JSON or NMEA)
func (g *GPSService) processGPSDMessage(message string) {
	// Check if this is an NMEA sentence (starts with $)
	if len(message) > 0 && message[0] == '$' {
		g.processNMEASentence(message)

		return
	}

	// Try to determine message type by checking class field
	var baseMsg struct {
		Class string `json:"class"`
	}

	err := json.Unmarshal([]byte(message), &baseMsg)
	if err != nil {
		return
	}

	switch baseMsg.Class {
	case gpsdClassTPV:
		var report TPVReport

		err := json.Unmarshal([]byte(message), &report)
		if err != nil {
			return
		}

		g.updatePosition(report)

	case gpsdClassSKY:
		var skyReport SKYReport

		err := json.Unmarshal([]byte(message), &skyReport)
		if err != nil {
			return
		}

		g.updateSatelliteInfo(skyReport)
	}
}

// processNMEASentence handles raw NMEA sentences from GPSD
func (g *GPSService) processNMEASentence(sentence string) {
	// Send NMEA to active devices if configured
	if g.Config != nil && g.Config.GetGNSSSendAsNMEA() {
		go g.sendRawNMEAToActiveDevices(sentence)
	}
}
