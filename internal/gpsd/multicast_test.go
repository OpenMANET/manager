package gpsd

import (
	"errors"
	"net"
	"testing"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"golang.org/x/net/ipv4"
)

type recordingMulticastWriter struct {
	ttl       int
	bridge    *net.Interface
	data      []byte
	address   net.Addr
	ttlErr    error
	bridgeErr error
	writeErr  error
}

func (w *recordingMulticastWriter) SetMulticastTTL(ttl int) error {
	w.ttl = ttl

	return w.ttlErr
}

func (w *recordingMulticastWriter) SetMulticastInterface(bridge *net.Interface) error {
	w.bridge = bridge

	return w.bridgeErr
}

func (w *recordingMulticastWriter) WriteTo(data []byte, _ *ipv4.ControlMessage, address net.Addr) (int, error) {
	w.data = append([]byte(nil), data...)
	w.address = address

	return len(data), w.writeErr
}

func TestConfigureATAKMulticast_UsesOpenMANETBridge(t *testing.T) {
	writer := &recordingMulticastWriter{}
	bridge := &net.Interface{Name: network.DefaultBridgeInterfaceName, Index: 42}

	if err := configureATAKMulticast(writer, bridge); err != nil {
		t.Fatalf("configureATAKMulticast() error = %v", err)
	}

	if writer.ttl != atakMulticastTTL {
		t.Errorf("multicast TTL = %d, want %d", writer.ttl, atakMulticastTTL)
	}

	if writer.bridge != bridge {
		t.Errorf("multicast interface = %p, want %p", writer.bridge, bridge)
	}
}

func TestConfigureATAKMulticast_ReturnsInterfaceError(t *testing.T) {
	writer := &recordingMulticastWriter{bridgeErr: errors.New("interface unavailable")}
	bridge := &net.Interface{Name: network.DefaultBridgeInterfaceName}

	if err := configureATAKMulticast(writer, bridge); err == nil {
		t.Fatal("configureATAKMulticast() error = nil, want interface error")
	}
}

func TestATAKMulticastSenderSend_WritesPacketToMulticastAddress(t *testing.T) {
	writer := &recordingMulticastWriter{}
	address := &net.UDPAddr{IP: net.ParseIP(config.ATAKSAAddress), Port: 6969}
	sender := &atakMulticastSender{packet: writer, address: address}
	payload := []byte("CoT")

	if err := sender.Send(payload); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if got, want := string(writer.data), string(payload); got != want {
		t.Errorf("written payload = %q, want %q", got, want)
	}

	if writer.address != address {
		t.Errorf("written address = %v, want %v", writer.address, address)
	}
}
