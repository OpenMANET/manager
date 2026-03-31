package batmanadv

import (
	"fmt"
	"syscall"
)

// ueventSocketConn wraps a raw netlink socket file descriptor for reading kobject uevents.
type ueventSocketConn struct {
	fd int
}

func (c *ueventSocketConn) Read(p []byte) (int, error) {
	n, _, err := syscall.Recvfrom(c.fd, p, 0)
	if err != nil {
		return 0, fmt.Errorf("recvfrom: %w", err)
	}

	return n, nil
}

func (c *ueventSocketConn) Close() error {
	if err := syscall.Close(c.fd); err != nil {
		return fmt.Errorf("close uevent socket: %w", err)
	}

	return nil
}

// SetReadDeadline sets a read timeout on the socket.
func (c *ueventSocketConn) SetReadDeadline(ms int) error {
	tv := syscall.Timeval{Sec: int64(ms / 1000), Usec: int64((ms % 1000) * 1000)} //nolint:unconvert

	if err := syscall.SetsockoptTimeval(c.fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("set uevent read deadline: %w", err)
	}

	return nil
}

// dialUeventSocket opens a NETLINK_KOBJECT_UEVENT socket.
func dialUeventSocket() (ueventConn, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_DGRAM, syscall.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return nil, fmt.Errorf("socket: %w", err)
	}

	addr := &syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Groups: 1, // KOBJECT_UEVENT multicast group
	}

	if err := syscall.Bind(fd, addr); err != nil {
		syscall.Close(fd)

		return nil, fmt.Errorf("bind: %w", err)
	}

	return &ueventSocketConn{fd: fd}, nil
}
