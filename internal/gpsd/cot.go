package gpsd

import (
	"encoding/xml"
	"fmt"
	"net"
	"net/netip"
	"os"
	"time"

	"github.com/coreywagehoft/go-tak/pkg/cot"
	"github.com/coreywagehoft/go-tak/pkg/cotproto"
	"github.com/mdlayher/arp"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/util/board"
	"golang.org/x/net/ipv4"
)

// SendLocationtoEUDs sends the current GPS location to any connected EUD clients.
// The eud devices are determined by the current dhcp leases.
// If no DHCP leases are found or no devices are reachable, it sends a CoT message to the ATAK SA multicast address.
func (g *GPSService) SendLocationtoEUDs() {
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

	// Track if we sent ANY message to ANY active device
	anyMessageSent := false

	// Send CoT messages to each active EUD device if configured
	// Send as CoT only if configured and NMEA sending is disabled
	if len(leases.DHCPLeases) > 0 && !g.Config.GetGNSSSendAsNMEA() {
		// loop through leases.DHCPleases and send location to each EUD
		for _, lease := range leases.DHCPLeases {
			// Send an ARP request to verify the EUD is online
			if !g.checkDeviceActive(lease.IPAddr) {
				g.Log.Debug().Str("ip", lease.IPAddr).Msg("Device not responding to ARP, skipping")
				continue
			}

			// Track success for this device
			deviceSuccess := false

			// Send CoT message as External GPS to the EUD
			if err := g.sendCoTTAsExternalGPS(lease.IPAddr); err != nil {
				g.Log.Error().Err(err).Str("ip", lease.IPAddr).Msg("Failed to send CoT to EUD")
			} else {
				deviceSuccess = true
			}

			// Count device as reached if either message succeeded
			if deviceSuccess {
				anyMessageSent = true
			}
		}
	}

	// Only send to multicast if no devices received any messages
	if !anyMessageSent {
		// Rate limit: send multicast messages once every 30 seconds to avoid flooding the network
		g.mu.Lock()
		if time.Since(g.lastMulticastTime) < cotMulticastRateLimit {
			g.mu.Unlock()
			return // Rate limited, exit early
		}
		g.lastMulticastTime = time.Now()
		g.mu.Unlock()

		g.Log.Debug().Msg("No reachable devices found, sending CoT to ATAK SA multicast address")
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
					return true
				}

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
		Ce:  pos.EPH,
		Le:  pos.EPV,
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

	// Marshal to XML
	xmlData, err := xml.Marshal(cot.ProtoToEvent(cotMsg))
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

	_, err = pconn.WriteTo(xmlData, nil, addr)
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
		Le:        pos.EPV,
		Ce:        pos.EPH,
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
