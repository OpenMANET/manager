package websocket

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/rs/zerolog"
)

const (
	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pongWait is the time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// pingPeriod sends pings at this interval. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize is the maximum message size allowed from peer.
	maxMessageSize = 64 * 1024 // 64 KB

	// sendBufferSize is the channel buffer for outgoing messages.
	sendBufferSize = 256
)

// MaxChannels is the maximum number of talkgroup channels supported.
const MaxChannels = 16

// Client represents a single WebSocket connection.
type Client struct {
	log        zerolog.Logger
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	remoteAddr string
	mu         sync.RWMutex
	rxEnabled  [MaxChannels]bool
	txEnabled  [MaxChannels]bool
}

// NewClient creates a new Client bound to the given hub and connection.
func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:        hub,
		conn:       conn,
		send:       make(chan []byte, sendBufferSize),
		remoteAddr: conn.RemoteAddr().String(),
		log:        logger.GetLogger("websocket.client"),
	}
}

// IsRXEnabled returns whether the client is subscribed to RX on the given channel.
func (c *Client) IsRXEnabled(channel byte) bool {
	if int(channel) >= MaxChannels {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.rxEnabled[channel]
}

// SetRX sets the RX subscription state for a channel.
func (c *Client) SetRX(channel byte, on bool) {
	if int(channel) >= MaxChannels {
		return
	}

	c.mu.Lock()
	c.rxEnabled[channel] = on
	c.mu.Unlock()
}

// SetTX sets the TX state for a channel.
func (c *Client) SetTX(channel byte, on bool) {
	if int(channel) >= MaxChannels {
		return
	}

	c.mu.Lock()
	c.txEnabled[channel] = on
	c.mu.Unlock()
}

// SetAllRX sets all channel RX states.
func (c *Client) SetAllRX(on bool) {
	c.mu.Lock()
	for i := range c.rxEnabled {
		c.rxEnabled[i] = on
	}
	c.mu.Unlock()
}

// SetAllTX sets all channel TX states.
func (c *Client) SetAllTX(on bool) {
	c.mu.Lock()
	for i := range c.txEnabled {
		c.txEnabled[i] = on
	}
	c.mu.Unlock()
}

// ReadPump pumps messages from the WebSocket connection to the hub.
// It must be called in a goroutine per client.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)

	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		c.log.Error().Err(err).Msg("failed to set read deadline")

		return
	}

	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.log.Error().Err(err).Msg("read error")
			}

			return
		}

		if messageType != websocket.BinaryMessage || len(data) == 0 {
			continue
		}

		c.hub.HandleMessage(c, data)
	}
}

// WritePump pumps messages from the send channel to the WebSocket connection.
// It must be called in a goroutine per client.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.log.Error().Err(err).Msg("failed to set write deadline")

				return
			}

			if !ok {
				// Hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})

				return
			}

			if err := c.conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
				c.log.Error().Err(err).Msg("write error")

				return
			}

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.log.Error().Err(err).Msg("failed to set write deadline")

				return
			}

			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Send queues a message for writing to the client's WebSocket connection.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		c.log.Warn().Str("remote", c.remoteAddr).Msg("send buffer full, dropping message")
	}
}

// RemoteAddr returns the client's remote address.
func (c *Client) RemoteAddr() string {
	return c.remoteAddr
}
