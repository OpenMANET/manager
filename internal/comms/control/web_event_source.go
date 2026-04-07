package control

import (
	"context"

	"github.com/rs/zerolog"
)

// WebEventSource implements EventSource for web-based PTT control. RPC
// handlers inject PTT events via Push; the comms Run loop consumes them
// from the channel returned by Events.
type WebEventSource struct {
	ch  chan PTTEvent
	log zerolog.Logger
}

// NewWebEventSource creates a WebEventSource with a small buffered channel.
func NewWebEventSource(log zerolog.Logger) *WebEventSource {
	return &WebEventSource{
		ch:  make(chan PTTEvent, 4),
		log: log,
	}
}

// Events implements EventSource. The returned channel is closed when ctx is
// canceled.
func (w *WebEventSource) Events(ctx context.Context) <-chan PTTEvent {
	go func() {
		<-ctx.Done()
		close(w.ch)
	}()

	return w.ch
}

// Push injects a PTT event from an external caller (e.g. an RPC handler).
// If the channel is full the event is dropped to avoid blocking the caller.
func (w *WebEventSource) Push(ev PTTEvent) {
	select {
	case w.ch <- ev:
	default:
		w.log.Warn().Msg("web: PTT event channel full; dropping event")
	}
}
