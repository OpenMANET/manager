package ptt

import "net"

// PacketWriter abstracts the UDP send path so the broadcast callback can be
// tested without a live socket.
type PacketWriter interface {
	Write(b []byte) (int, error)
}

// PacketReader abstracts the UDP receive path so receiveLoop can be exercised
// with pre-seeded byte sequences in tests.
type PacketReader interface {
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	Close() error
}
