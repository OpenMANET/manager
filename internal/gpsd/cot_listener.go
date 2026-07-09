package gpsd

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/coreywagehoft/go-tak/pkg/cot"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"golang.org/x/net/ipv4"
)

const (
	// cotListenerReadTimeout bounds each recv so the listener can notice
	// shutdown (g.done) without blocking forever on an idle multicast group.
	cotListenerReadTimeout = 2 * time.Second

	// cotListenerMaxDatagram is large enough for any CoT event this
	// project produces or consumes (mesh protobuf or XML), with headroom.
	cotListenerMaxDatagram = 8192

	// externalCoTSource is the config.GNSSSource string value that
	// selects an EUD's CoT broadcast as this node's position feed.
	externalCoTSource = "external_cot"
)

// startCoTListener joins the ATAK SA multicast group and, whenever the
// configured GNSS source is external_cot, adopts the position broadcast by
// a directly-connected end-user device (e.g. ATAK) as this node's own
// position. This lets a node with no local GNSS receiver still report a
// (typically more accurate) position on the mesh and in the topology view.
//
// The read loop runs for the life of the GPSService regardless of the
// configured source, so toggling gnss.source between internal and
// external_cot takes effect immediately without restarting anything.
func (g *GPSService) startCoTListener() {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%s", config.ATAKSAAddress, atakSAMulticastPort))
	if err != nil {
		g.Log.Error().Err(err).Msg("Failed to resolve CoT SA multicast address for external GPS listener")

		return
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: addr.Port})
	if err != nil {
		g.Log.Error().Err(err).Msg("Failed to open CoT multicast listener for external GPS source")

		return
	}
	defer conn.Close()

	pconn := ipv4.NewPacketConn(conn)
	if !joinMulticastOnAllInterfaces(pconn, addr, g.Log) {
		g.Log.Error().Msg("Failed to join CoT SA multicast group on any interface for external GPS source")

		return
	}

	buf := make([]byte, cotListenerMaxDatagram)

	for {
		select {
		case <-g.done:
			return
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(cotListenerReadTimeout)); err != nil {
			g.Log.Debug().Err(err).Msg("Failed to set CoT listener read deadline")
		}

		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			if isTimeout(err) {
				continue
			}

			g.Log.Debug().Err(err).Msg("CoT listener read error")

			continue
		}

		if !g.externalCoTSelected() {
			// Not adopting an external position right now; keep draining
			// the socket so the group stays joined and the buffer doesn't
			// back up once the setting is switched on.
			continue
		}

		g.handleIncomingCoT(buf[:n], src.IP.String())
	}
}

// isTimeout reports whether err is a network timeout, as produced by the
// read deadline set on every loop iteration.
func isTimeout(err error) bool {
	var ne net.Error

	return errors.As(err, &ne) && ne.Timeout()
}

// joinMulticastOnAllInterfaces joins addr's multicast group on every
// multicast-capable, up interface on the host, rather than a single
// implicit choice. A GNSS-less node has no default route (no HaLow HAT,
// no WAN uplink), so letting the kernel pick an interface (JoinGroup with
// a nil *net.Interface) can fail outright; explicitly joining on each
// candidate interface (br-lan today, br-ahwlan once a HaLow HAT is
// bridged in) is robust to whichever bridge actually carries EUD traffic.
// Returns true if the group was joined on at least one interface.
func joinMulticastOnAllInterfaces(pconn *ipv4.PacketConn, addr *net.UDPAddr, log zerolog.Logger) bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Error().Err(err).Msg("Failed to list network interfaces for CoT multicast join")

		return false
	}

	joined := false

	for i := range ifaces {
		iface := ifaces[i]

		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}

		if err := pconn.JoinGroup(&iface, addr); err != nil {
			log.Debug().Err(err).Str("interface", iface.Name).Msg("Failed to join CoT SA multicast group on interface")

			continue
		}

		joined = true
	}

	return joined
}

// handleIncomingCoT parses one CoT datagram received on the SA multicast
// group and, if it originated from a directly-connected EUD, adopts its
// position as this node's own.
func (g *GPSService) handleIncomingCoT(data []byte, srcIP string) {
	pos, ok := parseCoTPosition(data)
	if !ok {
		return
	}

	if !g.isKnownEUD(srcIP) {
		g.Log.Debug().
			Str("source_ip", srcIP).
			Str("uid", pos.uid).
			Msg("Ignoring CoT position from a sender that is not a directly-connected EUD")

		return
	}

	g.applyExternalPosition(pos)

	g.Log.Debug().
		Str("source_ip", srcIP).
		Str("uid", pos.uid).
		Float64("lat", pos.lat).
		Float64("lon", pos.lon).
		Msg("Adopted external GPS position from EUD CoT broadcast")

	// Re-announce this node's (now EUD-sourced) position to the rest of
	// the mesh/EUDs, same as it would with an onboard receiver. Guarded by
	// a single-flight flag: an EUD broadcasting SA faster than one
	// re-announce takes would otherwise stack up overlapping goroutines,
	// each doing a ubus lease lookup and an ARP probe per lease. Dropping
	// the extra kicks is correct — the next broadcast re-announces the
	// newer position anyway.
	if g.reannouncing.CompareAndSwap(false, true) {
		go func() {
			defer g.reannouncing.Store(false)

			g.SendIfRequiredAsCoT()
		}()
	}
}

// isKnownEUD reports whether ipAddr belongs to a device currently holding
// an active DHCP lease from this node — i.e. a directly-connected EUD, as
// opposed to a CoT relay from elsewhere on the mesh.
func (g *GPSService) isKnownEUD(ipAddr string) bool {
	leases, err := g.getDHCPLeases()
	if err != nil {
		g.Log.Debug().Err(err).Msg("Failed to read DHCP leases while validating CoT sender")

		return false
	}

	for _, lease := range leases.DHCPLeases {
		if lease.IPAddr == ipAddr {
			return true
		}
	}

	return false
}

// externalCoTSelected reports whether the configured GNSS source is an
// EUD's CoT broadcast rather than the local receiver. A nil Config reads
// as internal — the documented default for an unset source. The listener
// goroutine outlives any single caller, so it must not assume a Config
// was wired up: a panic here would take the whole daemon down.
func (g *GPSService) externalCoTSelected() bool {
	if g.Config == nil {
		return false
	}

	return g.Config.GetGNSSSource() == externalCoTSource
}

// getDHCPLeases calls the injected GetDHCPLeases override when set (tests),
// falling back to the real ubus-backed lookup otherwise.
func (g *GPSService) getDHCPLeases() (*network.DHCPLeasesResponse, error) {
	if g.GetDHCPLeases != nil {
		return g.GetDHCPLeases()
	}

	return network.GetCurrentDHCPLeases()
}

// cotPosition holds the fields extracted from an incoming CoT event that
// are relevant to adopting it as this node's position.
type cotPosition struct {
	uid    string
	lat    float64
	lon    float64
	hae    float64
	speed  float64
	course float64
}

// parseCoTPosition decodes a single CoT datagram, trying the TAK mesh
// protobuf framing first (magic byte 0xbf) and falling back to raw XML CoT.
// ok is false when the datagram is not a recognizable CoT position event.
func parseCoTPosition(data []byte) (cotPosition, bool) {
	if len(data) == 0 {
		return cotPosition{}, false
	}

	if data[0] == 0xbf {
		return parseCoTPositionProto(data)
	}

	return parseCoTPositionXML(data)
}

// parseCoTPositionProto decodes the TAK mesh protobuf framing used by
// ATAK's "Mesh SA" broadcast mode.
func parseCoTPositionProto(data []byte) (cotPosition, bool) {
	msg, _, err := cot.ReadProtoMesh(bufio.NewReader(bytes.NewReader(data)))
	if err != nil || msg.GetCotEvent() == nil {
		return cotPosition{}, false
	}

	event := msg.GetCotEvent()

	pos := cotPosition{
		uid: event.GetUid(),
		lat: event.GetLat(),
		lon: event.GetLon(),
		hae: event.GetHae(),
	}

	if track := event.GetDetail().GetTrack(); track != nil {
		pos.speed = track.GetSpeed()
		pos.course = track.GetCourse()
	}

	if pos.lat == 0 && pos.lon == 0 {
		return cotPosition{}, false
	}

	return pos, true
}

// parseCoTPositionXML decodes a traditional (version 0) XML CoT event, the
// format some ATAK configurations and older EUDs use instead of the mesh
// protobuf framing. Speed/course are not extracted from this path since
// they live in the free-form <detail> node; position is the essential
// field for adopting an external GPS source.
func parseCoTPositionXML(data []byte) (cotPosition, bool) {
	var event cot.Event
	if err := xml.Unmarshal(data, &event); err != nil {
		return cotPosition{}, false
	}

	if event.Point.Lat == 0 && event.Point.Lon == 0 {
		return cotPosition{}, false
	}

	return cotPosition{
		uid: event.UID,
		lat: event.Point.Lat,
		lon: event.Point.Lon,
		hae: event.Point.Hae,
	}, true
}
