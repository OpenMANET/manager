package blos

import (
	"fmt"
	"net"

	"github.com/openmanet/openmanetd/internal/config"
	"golang.org/x/net/ipv4"
)

// multicastJoiner is the signature of the operation that opens a UDP socket and
// joins one multicast group on a given interface. The returned *net.UDPConn must
// be held open by the caller for the group membership to remain active.
type multicastJoiner func(iface *net.Interface, group net.IP) (*net.UDPConn, error)

// realMulticastJoiner is the production multicastJoiner. It opens a UDP socket
// bound to the all-zeros address on an ephemeral port — its sole purpose is to
// hold the IGMP membership — then calls ipv4.PacketConn.JoinGroup.
func realMulticastJoiner(iface *net.Interface, group net.IP) (*net.UDPConn, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("open UDP socket for multicast join %s on %s: %w", group, iface.Name, err)
	}

	pc := ipv4.NewPacketConn(conn)
	if err := pc.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("join multicast group %s on %s: %w", group, iface.Name, err)
	}

	return conn, nil
}

// joinMulticastGroupsOnInterface joins all configured multicast group addresses
// on the named network interface and stores the resulting sockets in
// r.multicastConns so they remain open for the lifetime of the daemon.
//
// When cfg.CommsEnable is true, config.TalkGroupMcastAddr is skipped because the
// comms subsystem manages that group membership independently.
//
// On any error, all sockets opened during this call are closed before the error
// is returned. Sockets from prior calls are left untouched.
func (r *BLOS) joinMulticastGroupsOnInterface(ifaceName string) error {
	ifi, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("resolve interface %q for multicast join: %w", ifaceName, err)
	}

	joiner := r.mcastJoiner
	if joiner == nil {
		joiner = realMulticastJoiner
	}

	addrs := config.GetMulticastGroupAddresses()
	newConns := make([]*net.UDPConn, 0, len(addrs))

	for _, addrStr := range addrs {
		if r.cfg.CommsEnable && addrStr == config.TalkGroupMcastAddr {
			r.logger.Debug().
				Str("addr", addrStr).
				Msg("Skipping multicast group join: comms subsystem owns this group")

			continue
		}

		ip := net.ParseIP(addrStr)
		if ip == nil {
			_ = closeConns(newConns)

			return fmt.Errorf("invalid multicast group address %q", addrStr)
		}

		conn, joinErr := joiner(ifi, ip)
		if joinErr != nil {
			_ = closeConns(newConns)

			return joinErr
		}

		newConns = append(newConns, conn)

		r.logger.Debug().
			Str("addr", addrStr).
			Str("iface", ifaceName).
			Msg("Joined multicast group")
	}

	r.mu.Lock()
	r.multicastConns = append(r.multicastConns, newConns...)
	r.mu.Unlock()

	r.logger.Info().
		Int("count", len(newConns)).
		Str("iface", ifaceName).
		Msg("Multicast group memberships established")

	return nil
}

// closeConns closes each conn in the slice and returns the last non-nil error.
func closeConns(conns []*net.UDPConn) error {
	var lastErr error

	for _, c := range conns {
		if c != nil {
			if err := c.Close(); err != nil {
				lastErr = err
			}
		}
	}

	return lastErr
}
