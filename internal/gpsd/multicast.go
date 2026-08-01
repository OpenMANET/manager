package gpsd

import (
	"fmt"
	"net"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"golang.org/x/net/ipv4"
)

// multicastPacketWriter is the packet-level portion of an unconnected UDP
// socket used for ATAK SA multicast. Keeping it small makes the bridge and
// write contract independently testable.
type multicastPacketWriter interface {
	SetMulticastTTL(int) error
	SetMulticastInterface(*net.Interface) error
	WriteTo([]byte, *ipv4.ControlMessage, net.Addr) (int, error)
}

// atakMulticastSender sends ATAK SA packets through the OpenMANET bridge.
// The underlying UDP socket is intentionally unconnected: PacketConn.WriteTo
// is not valid on a socket created with net.DialUDP.
type atakMulticastSender struct {
	conn    *net.UDPConn
	packet  multicastPacketWriter
	address *net.UDPAddr
}

func newATAKMulticastSender() (*atakMulticastSender, error) {
	address, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(config.ATAKSAAddress, atakSAMulticastPort))
	if err != nil {
		return nil, fmt.Errorf("resolve ATAK SA multicast address: %w", err)
	}

	bridge, err := net.InterfaceByName(network.DefaultBridgeInterfaceName)
	if err != nil {
		return nil, fmt.Errorf("find ATAK multicast interface %q: %w", network.DefaultBridgeInterfaceName, err)
	}

	// ListenUDP creates an unconnected socket. This avoids both the route
	// dependency of DialUDP and ErrWriteToConnected when sending a packet.
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("open ATAK multicast socket: %w", err)
	}

	packet := ipv4.NewPacketConn(conn)
	if err := configureATAKMulticast(packet, bridge); err != nil {
		_ = conn.Close()

		return nil, err
	}

	return &atakMulticastSender{conn: conn, packet: packet, address: address}, nil
}

func configureATAKMulticast(packet multicastPacketWriter, bridge *net.Interface) error {
	if err := packet.SetMulticastTTL(atakMulticastTTL); err != nil {
		return fmt.Errorf("set ATAK multicast TTL: %w", err)
	}

	if err := packet.SetMulticastInterface(bridge); err != nil {
		return fmt.Errorf("set ATAK multicast interface %q: %w", bridge.Name, err)
	}

	return nil
}

func (s *atakMulticastSender) Send(data []byte) error {
	if _, err := s.packet.WriteTo(data, nil, s.address); err != nil {
		return fmt.Errorf("write ATAK multicast packet: %w", err)
	}

	return nil
}

func (s *atakMulticastSender) Close() error {
	return s.conn.Close()
}
