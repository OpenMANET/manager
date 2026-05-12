package websocket

import (
	"testing"
)

func TestClient_RXToggle(t *testing.T) {
	client := &Client{
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:1234",
	}

	if client.IsRXEnabled(1) {
		t.Error("channel 1 RX should be disabled by default")
	}

	client.SetRX(1, true)

	if !client.IsRXEnabled(1) {
		t.Error("channel 1 RX should be enabled after SetRX(1, true)")
	}

	client.SetRX(1, false)

	if client.IsRXEnabled(1) {
		t.Error("channel 1 RX should be disabled after SetRX(1, false)")
	}
}

func TestClient_TXToggle(t *testing.T) {
	client := &Client{
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:1234",
	}

	client.SetTX(3, true)
	// TX state is stored but not externally queryable; just verify no panic.
	client.SetTX(3, false)
}

func TestClient_SetAllRX(t *testing.T) {
	client := &Client{
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:1234",
	}

	client.SetAllRX(true)

	for i := byte(0); i < MaxChannels; i++ {
		if !client.IsRXEnabled(i) {
			t.Errorf("channel %d RX should be enabled after SetAllRX(true)", i)
		}
	}

	client.SetAllRX(false)

	for i := byte(0); i < MaxChannels; i++ {
		if client.IsRXEnabled(i) {
			t.Errorf("channel %d RX should be disabled after SetAllRX(false)", i)
		}
	}
}

func TestClient_SetAllTX(t *testing.T) {
	client := &Client{
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:1234",
	}

	// Should not panic with any combination.
	client.SetAllTX(true)
	client.SetAllTX(false)
}

func TestClient_BoundsCheck(t *testing.T) {
	client := &Client{
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:1234",
	}

	// Out-of-bounds channel should not panic.
	client.SetRX(255, true)

	if client.IsRXEnabled(255) {
		t.Error("out-of-bounds channel should return false")
	}

	client.SetTX(255, true)
}

func TestClient_Send(t *testing.T) {
	client := &Client{
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:1234",
	}

	msg := []byte{0x01, 0x02}
	client.Send(msg)

	select {
	case got := <-client.send:
		if len(got) != 2 || got[0] != 0x01 {
			t.Errorf("Send() message = %v, want %v", got, msg)
		}
	default:
		t.Error("Send() did not queue message")
	}
}

func TestClient_RemoteAddr(t *testing.T) {
	client := &Client{
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "192.168.1.1:5555",
	}
	if got := client.RemoteAddr(); got != "192.168.1.1:5555" {
		t.Errorf("RemoteAddr() = %q, want %q", got, "192.168.1.1:5555")
	}
}
