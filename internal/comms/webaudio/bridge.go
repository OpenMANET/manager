// Package webaudio hosts the web-mode audio bridge that replaces PortAudio's
// role when the comms control source is "web". The browser becomes the audio
// I/O device: it sends Opus frames over RPC (TX) and receives Opus frames
// popped from the per-port jitter buffer (RX). The bridge is a small plumbing
// layer between those RPC streams and the parent comms runtime — it never
// imports the parent package, keeping the dependency direction one-way.
package webaudio

import (
	"github.com/rs/zerolog"
)

// SendFn delivers an Opus payload from the browser to every send-enabled
// multicast port. The parent package captures this callback at bridge
// construction so webaudio has no knowledge of ports or runtime state.
type SendFn func(opusData []byte)

// Bridge connects the web RPC streaming handlers to the comms runtime.
//
//   - TX path: the RPC handler calls InjectTxFrame with Opus bytes from
//     the browser; the bridge forwards them via SendFn to all
//     send-enabled multicast ports.
//   - RX path: webPlayoutLoop calls PushRxFrame with raw Opus payloads
//     from the jitter buffer; the RPC handler reads them from RxFrames
//     and streams them back to the browser.
type Bridge struct {
	log      zerolog.Logger
	send     SendFn
	rxFrames chan []byte
}

// NewBridge creates a bridge wired to the given send callback and logger.
// The rxFrames channel is sized for ~1 second of slack at 50 fps.
func NewBridge(log zerolog.Logger, send SendFn) *Bridge {
	return &Bridge{
		log:      log,
		send:     send,
		rxFrames: make(chan []byte, 50),
	}
}

// InjectTxFrame forwards a raw Opus frame from the web client to all
// send-enabled multicast ports via the bound SendFn. If the SendFn is
// nil the call is a no-op so a partially constructed bridge does not
// panic during early shutdown.
func (b *Bridge) InjectTxFrame(opusData []byte) {
	if b == nil || b.send == nil {
		return
	}

	b.send(opusData)
}

// RxFrames returns a receive-only channel that delivers Opus frames from
// the mesh to the RPC handler.
func (b *Bridge) RxFrames() <-chan []byte {
	if b == nil {
		return nil
	}

	return b.rxFrames
}

// PushRxFrame delivers a raw Opus payload for the web client. The call
// is non-blocking; if the channel is full the frame is silently dropped.
func (b *Bridge) PushRxFrame(opusData []byte) {
	if b == nil {
		return
	}

	select {
	case b.rxFrames <- opusData:
	default:
		b.log.Debug().Msg("web: RX frame channel full; dropping")
	}
}
