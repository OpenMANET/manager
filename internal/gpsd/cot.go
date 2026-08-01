package gpsd

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"net/netip"
	"os"
	"time"

	"github.com/coreywagehoft/go-tak/pkg/cot"
	"github.com/coreywagehoft/go-tak/pkg/cotproto"
	"github.com/mdlayher/arp"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/tak"
	"github.com/openmanet/openmanetd/internal/util/board"
)

// SendIfRequiredAsCoT sends the GPS position as a Cursor-on-Target (CoT) message to End User Devices (EUDs).
// It first validates that a GPS position is available, then retrieves active DHCP leases to identify
// potential EUD recipients. The method checks each leased device for activity for diagnostics and
// publishes the CoT message to the ATAK Situational Awareness (SA) multicast address, subject to
// rate limiting (once every 30 seconds) to prevent network flooding. Publishing regardless of DHCP
// lease state is necessary for ATAK clients to receive advertised node capabilities such as video.
//
// The method performs the following steps:
//  1. Validates GPS position availability
//  2. Records reachable DHCP EUDs for diagnostics when lease data is available
//  3. Sends to the SA multicast group (rate-limited)
//
// Errors are logged but do not halt execution; the method returns early on validation failures.
func (g *GPSService) SendIfRequiredAsCoT() {
	// Check if we have a valid GPS position
	if !g.IsValid() {
		g.Log.Warn().Msg("No valid GPS position to send to EUDs")

		return
	}

	// Track whether any EUD is active for diagnostics. CoT is still sent to the
	// SA multicast group so connected ATAK clients receive node capabilities.
	deviceActive := false

	leases, err := network.GetCurrentDHCPLeases()
	if err != nil {
		g.Log.Debug().Err(err).Msg("Unable to inspect DHCP leases before CoT multicast")
	} else if len(leases.DHCPLeases) > 0 {
		// Check all DHCP leases for active EUDs.
		// loop through leases.DHCPleases and send location to each EUD
		for _, lease := range leases.DHCPLeases {
			// Send an ARP request to verify the EUD is online
			if g.checkDeviceActive(lease.IPAddr) {
				// Device is active
				deviceActive = true
			}
		}
	}

	// Rate limit: send multicast messages once every 30 seconds to avoid flooding the network.
	g.mu.Lock()
	if time.Since(g.lastMulticastTime) < cotMulticastRateLimit {
		g.mu.Unlock()

		return
	}

	g.lastMulticastTime = time.Now()
	g.mu.Unlock()

	if err := g.sendCoTPing(); err != nil {
		g.Log.Error().Err(err).Msg("Failed to send CoT ping to multicast")
	}

	g.Log.Debug().Bool("active_eud", deviceActive).Msg("Sending CoT to ATAK SA multicast address")

	if err := g.sendCoTToMulticast(); err != nil {
		g.Log.Error().Err(err).Msg("Failed to send CoT to multicast")
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
				err = client.SetDeadline(time.Now().Add(500 * time.Millisecond))
				if err != nil {
					g.Log.Debug().Err(err).Msg("Failed to set ARP deadline")
					client.Close()

					continue
				}

				// Perform ARP request using netip.Addr
				_, err = client.Resolve(ipAddrParsed)
				client.Close()

				return err == nil
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

	stream, streamErr := tak.DiscoverCameraStream(context.Background())
	if streamErr != nil {
		g.Log.Warn().Err(streamErr).Msg("Unable to advertise camera stream in ATAK")
	}

	messages, err := tak.BuildNodeMessages(time.Now(), tak.Position{
		Altitude:        pos.Altitude,
		Ce:              pos.EPH,
		GeoidSeparation: pos.GeoidSeparation,
		Lat:             pos.Latitude,
		Le:              pos.EPV,
		Lon:             pos.Longitude,
		Speed:           pos.Speed,
		Track:           pos.Track,
	}, tak.Node{
		Callsign: hostname,
		Platform: platformName,
		UID:      hostname,
	}, stream)
	if err != nil {
		return fmt.Errorf("build ATAK CoT messages: %w", err)
	}

	sender, err := newATAKMulticastSender()
	if err != nil {
		return err
	}
	defer sender.Close()

	for _, message := range messages {
		data, marshalErr := cot.MakeProtoMeshPacketV1(message)
		if marshalErr != nil {
			return fmt.Errorf("marshal CoT protobuf: %w", marshalErr)
		}

		if sendErr := sender.Send(data); sendErr != nil {
			return sendErr
		}
	}

	g.Log.Debug().
		Str("callsign", messages[0].GetCotEvent().GetUid()).
		Int("events", len(messages)).
		Float64("lat", pos.Latitude).
		Float64("lon", pos.Longitude).
		Float64("alt", pos.Altitude).
		Str("address", fmt.Sprintf("%s:%s", config.ATAKSAAddress, atakSAMulticastPort)).
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
				Geopointsrc: gnssSourceGPS,
				Altsrc:      gnssSourceGPS,
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

func (g *GPSService) sendCoTPing() error {
	// Marshal to bytes to send as protobuf
	data, err := cot.MakeProtoMeshPacketV1(cot.MakePing("openmanet-ping"))
	if err != nil {
		return fmt.Errorf("failed to marshal CoT protobuf: %w", err)
	}

	sender, err := newATAKMulticastSender()
	if err != nil {
		return err
	}
	defer sender.Close()

	if err := sender.Send(data); err != nil {
		return err
	}

	return nil
}
