package websocket

import (
	"context"
	"sync"

	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/rs/zerolog"
)

// MessageHandler is called by the hub when a client sends a binary message.
// Implementations must not retain the data slice after returning.
type MessageHandler func(client *Client, data []byte)

// Hub maintains the set of active clients and broadcasts messages.
type Hub struct {
	log        zerolog.Logger
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	handler    MessageHandler
	mu         sync.RWMutex
}

// NewHub creates a new Hub with the given message handler.
func NewHub(handler MessageHandler) *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client, 16),
		unregister: make(chan *Client, 16),
		broadcast:  make(chan []byte, 256),
		handler:    handler,
		log:        logger.GetLogger("websocket.hub"),
	}
}

// Run starts the hub's event loop. It should be called in a goroutine.
// It returns when ctx is canceled, closing all client send channels.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			h.mu.Unlock()

			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = struct{}{}
			h.mu.Unlock()
			h.log.Info().Str("remote", client.remoteAddr).Msg("client registered")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.log.Info().Str("remote", client.remoteAddr).Msg("client unregistered")

		case message := <-h.broadcast:
			h.mu.RLock()

			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client send buffer full; schedule disconnect.
					select {
					case h.unregister <- client:
					default:
						go func(c *Client) {
							h.unregister <- c
						}(client)
					}
				}
			}

			h.mu.RUnlock()
		}
	}
}

// BroadcastAudioRX sends an audio RX frame to all clients subscribed to the given channel.
func (h *Hub) BroadcastAudioRX(channel byte, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client.IsRXEnabled(channel) {
			select {
			case client.send <- data:
			default:
				select {
				case h.unregister <- client:
				default:
					go func(c *Client) {
						h.unregister <- c
					}(client)
				}
			}
		}
	}
}

// RelayToOthers sends data to all clients except the sender.
func (h *Hub) RelayToOthers(sender *Client, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client == sender {
			continue
		}

		if client.IsRXEnabled(data[1]) {
			select {
			case client.send <- data:
			default:
				select {
				case h.unregister <- client:
				default:
					go func(c *Client) {
						h.unregister <- c
					}(client)
				}
			}
		}
	}
}

// BroadcastRaw sends raw bytes to all connected clients.
func (h *Hub) BroadcastRaw(data []byte) {
	h.broadcast <- data
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return len(h.clients)
}

// Register queues a client for registration.
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister queues a client for removal.
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

// HandleMessage dispatches a message from a client to the configured handler.
func (h *Hub) HandleMessage(client *Client, data []byte) {
	if h.handler != nil {
		h.handler(client, data)
	}
}
