package comms

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/openmanet/openmanetd/internal/comms/device"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
)

// rtpMulticastTTL is the IP TTL set on outgoing RTP/RTCP multicast packets.
// A value of 1 restricts packets to the local subnet; increase to allow
// traversal across routed multicast hops.
const rtpMulticastTTL = 1

// rxSocketBufBytes is the requested SO_RCVBUF size for the RTP receive
// socket. 1 MiB absorbs bursty mesh arrivals when receiveLoop is briefly
// scheduled out (GC, scheduler hand-off, neighbor goroutine). At ~100-byte
// Opus payloads this is roughly 10000 frames of headroom — far more than
// any realistic stall. The kernel may clamp the actual value at
// net.core.rmem_max (typically 208 KB on stock Linux, but embedded targets
// usually raise this in sysctl); the post-set verification log records
// what we actually got so undersized rmem_max is observable.
const rxSocketBufBytes = 1 << 20

// listenRTPReceiver opens a UDP socket bound to addr with SO_REUSEPORT enabled.
//
// SO_REUSEPORT lets a second socket bind to the same port while the current
// receiver is still open. This is required for UpdateMulticastEndpoint when
// the port does not change: buildSinglePortChannel must be able to acquire
// the new socket before the old one is closed.
func listenRTPReceiver(addr *net.UDPAddr) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			})
		},
	}

	pc, err := lc.ListenPacket(context.Background(), "udp4", addr.String())
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()

		return nil, errors.New("listenRTPReceiver: unexpected PacketConn type")
	}

	return conn, nil
}

// setMulticastTTL sets the IP multicast TTL on a UDP socket.
func setMulticastTTL(conn *net.UDPConn, ttl int) error {
	if err := ipv4.NewPacketConn(conn).SetMulticastTTL(ttl); err != nil {
		return fmt.Errorf("set multicast TTL: %w", err)
	}

	return nil
}

// getReadBufferBytes returns the kernel's actual SO_RCVBUF for conn. Linux
// reports the doubled value (the kernel adds bookkeeping overhead to
// whatever was requested), so the returned value is typically twice the
// argument passed to SetReadBuffer.
func getReadBufferBytes(conn *net.UDPConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("syscall conn: %w", err)
	}

	var (
		val     int
		sockErr error
	)

	if controlErr := raw.Control(func(fd uintptr) {
		val, sockErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
	}); controlErr != nil {
		return 0, fmt.Errorf("control: %w", controlErr)
	}

	if sockErr != nil {
		return 0, fmt.Errorf("getsockopt SO_RCVBUF: %w", sockErr)
	}

	return val, nil
}

// boolPtrVal dereferences p when non-nil; otherwise returns fallback.
// Used to distinguish "not set" from false in McastPortConfig.Init* fields.
func boolPtrVal(p *bool, fallback bool) bool {
	if p != nil {
		return *p
	}

	return fallback
}

// buildSinglePortChannel opens all sockets and creates the RTP session for one
// McastPortConfig entry. Directions (Send / Receive) are reflected by which
// fields are populated: a Send=false port has nil sender/rtcpSend/rtpSess; a
// Receive=false port has nil receiver. The runtime atomic direction flags are
// initialized from mpc.InitSendEnabled / InitReceiveEnabled (falling back to
// mpc.Send / Receive) so the hot paths can read them without locking.
//
// On any failure the deferred rollback closes whichever sockets and sessions
// were already attached to pc and returns (nil, err) to the caller; the
// individual error sites only need to assign err and bare-return.
func (cfg *CommsConfig) buildSinglePortChannel(
	mpc McastPortConfig,
	localIP string,
	ifi *net.Interface,
	ssrc uint32,
) (pc *PortChannel, err error) {
	pc = &PortChannel{cfg: mpc}
	pc.RxGate.Threshold = cfg.HalfDuplexThreshold
	pc.SendEnabled.Store(boolPtrVal(mpc.InitSendEnabled, mpc.Send))
	pc.ReceiveEnabled.Store(boolPtrVal(mpc.InitReceiveEnabled, mpc.Receive))

	defer func() {
		if err != nil {
			pc.closePartial()
			pc = nil
		}
	}()

	if mpc.Send {
		// ── RTP sender ─────────────────────────────────────────────────
		dst := &net.UDPAddr{IP: net.ParseIP(mpc.Address), Port: mpc.Port}
		src := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		sendConn, dialErr := net.DialUDP("udp4", src, dst)
		if dialErr != nil {
			err = fmt.Errorf("dial RTP sender %s:%d: %w", mpc.Address, mpc.Port, dialErr)

			return
		}

		pc.Sender = rtp.NewSwappableSender(sendConn)

		if ttlErr := setMulticastTTL(sendConn, rtpMulticastTTL); ttlErr != nil {
			err = fmt.Errorf("set multicast TTL on RTP sender %s:%d: %w", mpc.Address, mpc.Port, ttlErr)

			return
		}

		// ── RTCP sender ────────────────────────────────────────────────
		rtcpDst := &net.UDPAddr{IP: net.ParseIP(mpc.Address), Port: mpc.Port + 1}
		rtcpSrc := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		rtcpConn, dialErr := net.DialUDP("udp4", rtcpSrc, rtcpDst)
		if dialErr != nil {
			err = fmt.Errorf("dial RTCP sender %s:%d: %w", mpc.Address, mpc.Port+1, dialErr)

			return
		}

		pc.RTCPSend = rtp.NewSwappableSender(rtcpConn)

		if ttlErr := setMulticastTTL(rtcpConn, rtpMulticastTTL); ttlErr != nil {
			err = fmt.Errorf("set multicast TTL on RTCP sender %s:%d: %w", mpc.Address, mpc.Port+1, ttlErr)

			return
		}

		sess, sessErr := rtp.NewSession(ssrc, pc.Sender, pc.RTCPSend, cfg.Log)
		if sessErr != nil {
			err = fmt.Errorf("pion RTP session for %s:%d: %w", mpc.Address, mpc.Port, sessErr)

			return
		}

		pc.RTPSess = sess

		cfg.Log.Debug().Msgf("comms: RTP sender %s:%d  RTCP %s:%d", mpc.Address, mpc.Port, mpc.Address, mpc.Port+1)
	}

	if mpc.Receive {
		// ── RTP receiver ────────────────────────────────────────────────
		// SO_REUSEPORT lets UpdateMulticastEndpoint open a replacement socket
		// on the same port while the current receiver is still running.
		recvConn, listenErr := listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: mpc.Port})
		if listenErr != nil {
			err = fmt.Errorf("listen RTP receiver %s:%d: %w", mpc.Address, mpc.Port, listenErr)

			return
		}

		pc.Receiver = rtp.NewSwappableReceiver(recvConn)
		pc.Jitter = rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth)

		if bufErr := recvConn.SetReadBuffer(rxSocketBufBytes); bufErr != nil {
			err = fmt.Errorf("set RTP read buffer: %w", bufErr)

			return
		}

		// Verify what the kernel actually granted us. Linux clamps SO_RCVBUF
		// at net.core.rmem_max and silently caps the request, so logging the
		// observed value lets an operator see whether sysctl is undersized
		// for the desired audio safety margin.
		if got, gErr := getReadBufferBytes(recvConn); gErr == nil {
			cfg.Log.Debug().
				Int("requested_bytes", rxSocketBufBytes).
				Int("actual_bytes", got).
				Str("addr", mpc.Address).
				Int("port", mpc.Port).
				Msg("comms: rx socket buffer")
		}

		if joinErr := device.JoinMulticastGroup(ifi, recvConn, net.ParseIP(mpc.Address)); joinErr != nil {
			err = joinErr

			return
		}

		cfg.Log.Debug().Msgf("comms: RTP receiver port %d", mpc.Port)
	}

	return pc, nil
}

// buildNetwork opens sockets for every McastPortConfig entry and returns the
// assembled PortChannel slice plus the local IP address of cfg.Iface.
//
// The SSRC used for all Send-enabled ports is derived from cfg.RtpID (or
// localIP as fallback), keeping transmissions from this node identifiable
// across talk groups.
func (cfg *CommsConfig) buildNetwork() ([]*PortChannel, string, error) {
	localIP, ifi, err := device.IfaceIPv4(cfg.Iface)
	if err != nil {
		return nil, "", err
	}

	cfg.Log.Debug().Msgf("comms: interface %s localIP %s", cfg.Iface, localIP)

	rtpID := cfg.RtpID
	if rtpID == "" {
		rtpID = localIP
	}

	ssrc := rtp.SSRCFromID(rtpID)

	ports := make([]*PortChannel, 0, len(cfg.McastPorts))

	for _, mpc := range cfg.McastPorts {
		pc, err := cfg.buildSinglePortChannel(mpc, localIP, ifi, ssrc)
		if err != nil {
			// Clean up already-built channels before propagating the error.
			for _, built := range ports {
				built.closePartial()
			}

			return nil, "", err
		}

		ports = append(ports, pc)
	}

	return ports, localIP, nil
}

// replaceNetwork atomically swaps the packet-level I/O connections for port 0
// and closes the old connections. newSender, newRTCPSender, or newReceiver may
// be nil when that direction is not applicable to the port. Closing the old
// receiver unblocks any in-flight ReadFromUDP in receiveLoop.
func (cfg *CommsConfig) replaceNetwork(
	rt *CommsRuntime,
	newSender PacketWriter,
	newRTCPSender PacketWriter,
	newReceiver PacketReader,
	newLocalIP string,
) {
	pc := rt.Ports[0]

	if pc.Receiver != nil && newReceiver != nil {
		old := pc.Receiver.Swap(newReceiver)
		_ = old.Close()
	}

	if pc.Sender != nil && newSender != nil {
		// Deferred close: the lock-free Write path on SwappableSender
		// cannot be drained synchronously, so the previous underlying
		// connection is closed after rtp.SwapCloseGrace to let any in-flight
		// sendto(2) on the old fd finish first.
		pc.Sender.SwapAndDeferClose(newSender)
	}

	if pc.RTCPSend != nil && newRTCPSender != nil {
		pc.RTCPSend.SwapAndDeferClose(newRTCPSender)
	}

	rt.LocalIP.Store(&newLocalIP)
}
