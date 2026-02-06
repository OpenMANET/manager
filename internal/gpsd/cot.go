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

// SendIfRequiredAsCoT sends the GPS position as a Cursor-on-Target (CoT) message to End User Devices (EUDs).
// It first validates that a GPS position is available, then retrieves active DHCP leases to identify
// potential EUD recipients. The method checks each leased device for activity and attempts to send
// the CoT message to active devices. If no active devices are found, it falls back to sending a
// CoT message to the ATAK Situational Awareness (SA) multicast address, subject to rate limiting
// (once every 30 seconds) to prevent network flooding.
//
// The method performs the following steps:
//  1. Validates GPS position availability
//  2. Retrieves current DHCP leases
//  3. Checks each leased device for activity via ARP
//  4. If no active devices are found, sends to multicast (rate-limited)
//
// Errors are logged but do not halt execution; the method returns early on validation failures.
func (g *GPSService) SendIfRequiredAsCoT() {
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

	// Track have ANY active device
	deviceActive := false

	// Send CoT messages to each active EUD device if configured
	// Send as CoT only if configured and NMEA sending is disabled
	if len(leases.DHCPLeases) > 0 {
		// loop through leases.DHCPleases and send location to each EUD
		for _, lease := range leases.DHCPLeases {
			// Send an ARP request to verify the EUD is online
			if g.checkDeviceActive(lease.IPAddr) {
				// Device is active
				deviceActive = true
			}
		}
	}

	// Only send to multicast if no devices received any messages
	if !deviceActive {
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

	// Calculate Height Above Ellipsoid (HAE)
	// HAE = MSL altitude + Geoid Separation
	hae := pos.Altitude
	if pos.GeoidSeparation != 0 {
		hae = pos.Altitude + pos.GeoidSeparation
	}

	// Create CoT Message
	takMsg := &cotproto.TakMessage{
		CotEvent: &cotproto.CotEvent{
			Type:      cot.TypeTeam,
			Uid:       hostname,
			SendTime:  cot.TimeToMillis(time.Now()),
			StartTime: cot.TimeToMillis(time.Now()),
			StaleTime: cot.TimeToMillis(time.Now().Add(defaultStaleDuration)),
			How:       cot.HowDefault,
			Lat:       pos.Latitude,
			Lon:       pos.Longitude,
			Hae:       hae,
			Ce:        pos.EPH,
			Le:        pos.EPV,
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
		},
	}

	// Marshal to bytes to send as protobuf
	data, err := cot.MakeProtoMeshPacketV1(takMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal CoT protobuf: %w", err)
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

	_, err = pconn.WriteTo(data, nil, addr)
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
