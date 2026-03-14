package comms

import (
	"github.com/rs/zerolog"
)

// WebAudioBridge connects the web RPC streaming handlers to the comms
// runtime. It replaces PortAudio's role when the control source is "web":
//
//   - TX path: the RPC handler calls InjectTxFrame with Opus bytes from the
//     browser; the bridge forwards them to all send-enabled multicast ports.
//   - RX path: the modified playoutLoop calls PushRxFrame with raw Opus
//     payloads from the jitter buffer; the RPC handler reads them from
//     RxFrames and streams them back to the browser.
type WebAudioBridge struct {
	cfg      *CommsConfig
	rt       *CommsRuntime
	log      zerolog.Logger
	rxFrames chan []byte
}

// NewWebAudioBridge creates a bridge wired to the given config and runtime.
func NewWebAudioBridge(cfg *CommsConfig, rt *CommsRuntime, log zerolog.Logger) *WebAudioBridge {
	return &WebAudioBridge{
		cfg:      cfg,
		rt:       rt,
		log:      log,
		rxFrames: make(chan []byte, 50),
	}
}

// InjectTxFrame sends a raw Opus frame from the web client to all
// send-enabled multicast ports via RTP, bypassing PortAudio and the local
// Opus encoder entirely.
func (b *WebAudioBridge) InjectTxFrame(opusData []byte) {
	b.cfg.sendToAllPorts(b.rt, opusData)
}

// RxFrames returns a receive-only channel that delivers Opus frames from
// the mesh. The RPC handler reads from this channel to stream audio back
// to the web client.
func (b *WebAudioBridge) RxFrames() <-chan []byte {
	return b.rxFrames
}

// PushRxFrame delivers a raw Opus payload for the web client. The call is
// non-blocking; if the channel is full the frame is silently dropped.
func (b *WebAudioBridge) PushRxFrame(opusData []byte) {
	select {
	case b.rxFrames <- opusData:
	default:
		b.log.Debug().Msg("web: RX frame channel full; dropping")
	}
}
