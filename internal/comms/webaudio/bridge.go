// Package webaudio hosts the web-mode audio bridge that replaces the malgo
// pipeline when the comms control source is "web". The browser becomes the audio
// I/O device: it sends Opus frames over RPC (TX) and receives Opus frames
// popped from the per-port jitter buffer (RX). The bridge is a small plumbing
// layer between those RPC streams and the parent comms runtime — it never
// imports the parent package, keeping the dependency direction one-way.
package webaudio

import (
	"sync/atomic"

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

	// consumers counts the RPC streams currently reading RxFrames. The
	// producer side (webPlayoutLoop) checks HasConsumer before doing any
	// per-frame work, so an idle web node (no browser tab attached) pays
	// nothing for RX traffic beyond draining the jitter buffer.
	consumers atomic.Int32

	// Diagnostic counters for the RX side. All are monotonic since
	// bridge construction; consumers compute deltas across reporting
	// windows. RxPushIn counts every PushRxFrame call (frames offered
	// by webPlayoutLoop); RxPushDrop counts the subset that the
	// non-blocking channel send dropped because rxFrames was full.
	// RxGatedNoConsumer counts frames the playout drain discarded
	// without offering because no consumer was attached.
	RxPushIn          atomic.Int64
	RxPushDrop        atomic.Int64
	RxGatedNoConsumer atomic.Int64
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

// AddConsumer registers an RPC stream as a reader of RxFrames. The producer
// side only does per-frame work while at least one consumer is registered.
// Callers must pair every AddConsumer with exactly one RemoveConsumer
// (typically via defer) when the stream ends.
func (b *Bridge) AddConsumer() {
	if b == nil {
		return
	}

	b.consumers.Add(1)
}

// RemoveConsumer deregisters an RPC stream previously registered with
// AddConsumer.
func (b *Bridge) RemoveConsumer() {
	if b == nil {
		return
	}

	b.consumers.Add(-1)
}

// HasConsumer reports whether at least one RPC stream is currently reading
// RxFrames.
func (b *Bridge) HasConsumer() bool {
	return b != nil && b.consumers.Load() > 0
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

	b.RxPushIn.Add(1)

	select {
	case b.rxFrames <- opusData:
	default:
		b.RxPushDrop.Add(1)
		b.log.Debug().Msg("web: RX frame channel full; dropping")
	}
}
