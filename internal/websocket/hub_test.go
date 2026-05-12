package websocket

import (
	"context"
	"testing"
	"time"
)

func TestHub_RegisterUnregister(t *testing.T) {
	received := make(chan []byte, 10)

	hub := NewHub(func(c *Client, data []byte) {
		received <- data
	})
	go hub.Run(context.Background())

	// Give hub goroutine time to start.
	time.Sleep(10 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Fatalf("initial client count = %d, want 0", hub.ClientCount())
	}

	// Create a fake client (no actual websocket conn needed for registration test).
	client := &Client{
		hub:        hub,
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:1234",
	}

	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Fatalf("client count after register = %d, want 1", hub.ClientCount())
	}

	hub.Unregister(client)
	time.Sleep(10 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Fatalf("client count after unregister = %d, want 0", hub.ClientCount())
	}
}

func TestHub_BroadcastRaw(t *testing.T) {
	hub := NewHub(nil)
	go hub.Run(context.Background())

	time.Sleep(10 * time.Millisecond)

	client := &Client{
		hub:        hub,
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:5678",
	}
	hub.Register(client)
	time.Sleep(10 * time.Millisecond)

	msg := []byte{0x01, 0x02, 0x03}
	hub.BroadcastRaw(msg)

	select {
	case got := <-client.send:
		if len(got) != 3 || got[0] != 0x01 {
			t.Errorf("broadcast message = %v, want %v", got, msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for broadcast")
	}
}

func TestHub_BroadcastAudioRX_Filtered(t *testing.T) {
	hub := NewHub(nil)
	go hub.Run(context.Background())

	time.Sleep(10 * time.Millisecond)

	// Client subscribed to channel 1 only.
	subscribedClient := &Client{
		hub:        hub,
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "sub:1111",
	}
	subscribedClient.SetRX(1, true)

	// Client not subscribed to channel 1.
	unsubscribedClient := &Client{
		hub:        hub,
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "unsub:2222",
	}

	hub.Register(subscribedClient)
	hub.Register(unsubscribedClient)
	time.Sleep(10 * time.Millisecond)

	msg := []byte{0x01, 0x01, 0xAA}
	hub.BroadcastAudioRX(1, msg)

	// Subscribed client should receive.
	select {
	case <-subscribedClient.send:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("subscribed client did not receive broadcast")
	}

	// Unsubscribed client should not receive.
	select {
	case <-unsubscribedClient.send:
		t.Error("unsubscribed client received broadcast")
	case <-time.After(50 * time.Millisecond):
		// OK
	}
}

func TestHub_HandleMessage(t *testing.T) {
	var (
		receivedData   []byte
		receivedClient *Client
	)

	hub := NewHub(func(c *Client, data []byte) {
		receivedClient = c
		receivedData = make([]byte, len(data))
		copy(receivedData, data)
	})

	client := &Client{
		hub:        hub,
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: "test:9999",
	}

	msg := []byte{OpcodeAudioTX, 0xDE, 0xAD}
	hub.HandleMessage(client, msg)

	if receivedClient != client {
		t.Error("handler did not receive correct client")
	}

	if len(receivedData) != 3 || receivedData[0] != OpcodeAudioTX {
		t.Errorf("handler received %v, want %v", receivedData, msg)
	}
}
