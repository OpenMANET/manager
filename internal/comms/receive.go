package comms

import (
	"context"
	"errors"
	"net"
	"time"
)

// maxConsecutivePLC caps the number of consecutive Packet-Loss-Concealment
// frames the playout path will emit before falling back to silence. Opus PLC
// is designed for short losses but stays acceptable up to ~200 ms; beyond
// that the synthesized output becomes increasingly robotic, so silence is
// preferable. With a 20 ms frame, 10 frames ≈ 200 ms. The previous 100 ms
// cap was conservative and bailed out of PLC mid-burst on transient mesh
// losses, producing audible muted gaps.
const maxConsecutivePLC = 10

// concealRecentWindow is the inter-arrival window during which a missing
// frame is treated as a transient gap (PLC) rather than the end of the
// stream (silence). It matches maxConsecutivePLC × 20 ms so the two
// concealment heuristics agree on what counts as "recent": shorter, and
// popOrConceal would hand back genuine silence even though playoutOneFrame
// is still willing to PLC.
const concealRecentWindow = 200 * time.Millisecond

// zeroFloat32 fills a float32 slice with zeros. Retained for any legacy
// consumer boundaries that still deal in float32.
func zeroFloat32(out []float32) {
	for i := range out {
		out[i] = 0
	}
}

// zeroInt16 fills an int16 slice with zeros. Used by the playout callback to
// emit silence into the PortAudio int16 output buffer.
func zeroInt16(out []int16) {
	for i := range out {
		out[i] = 0
	}
}

// rxActiveThreshold is retained as the historical alias for the default
// half-duplex threshold. New code should use defaultHalfDuplexThreshold or
// the per-port halfDuplexGate threshold.
const rxActiveThreshold = defaultHalfDuplexThreshold

// isReceivingRemote returns true when a valid RTP packet was received from a
// remote peer within the half-duplex window on any send-enabled port. This is
// the receive-side component of half-duplex: transmission must not begin
// while a shared send+receive channel is actively carrying incoming audio.
func (cfg *CommsConfig) isReceivingRemote(rt *CommsRuntime) bool {
	for _, pc := range rt.ports {
		if !pc.sendEnabled.Load() {
			continue
		}

		if pc.rxGate.active() {
			return true
		}
	}

	return false
}

// ─── Receive path ─────────────────────────────────────────────────────────────

// receiveLoop reads datagrams from pc.receiver, parses them as RTP packets
// using pion/rtp, and pushes the payloads into the per-port jitter buffer.
//
// In non-web mode the PortAudio output callback is the consumer (driven by
// the audio hardware clock); no playout goroutine is spawned. In web mode a
// stripped-down webPlayoutLoop forwards raw Opus payloads to the WebAudioBridge.
//
// The loop discards packets from our own IP (loopback prevention) unless
// cfg.Loopback is true, and skips queuing frames when pc.receiveEnabled is false.
func (cfg *CommsConfig) receiveLoop(ctx context.Context, pc *portChannel, rt *CommsRuntime) { //nolint:gocognit
	buf := make([]byte, 1500)

	// pc.jitter is allocated in buildSinglePortChannel for receive-capable
	// ports. Test code that constructs portChannel directly may leave it
	// nil, in which case we allocate one here so the loop is self-sufficient.
	if pc.jitter == nil {
		pc.jitter = NewRTPJitterBuffer(JitterPrebufferPackets, JitterMaxDepth)
	}

	jitter := pc.jitter

	if rt.webBridge != nil {
		go cfg.webPlayoutLoop(ctx, jitter, rt)
	}

	// cachedLocalIP caches the parsed form of rt.localIP so that the loopback
	// filter can use a byte-level net.IP.Equal comparison instead of calling
	// src.IP.String() (which allocates) on every received packet. The cached
	// value is refreshed only when the string changes (i.e. on endpoint swap).
	var (
		cachedLocalIPStr string
		cachedLocalIP    net.IP
	)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, src, err := pc.receiver.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				// When UpdateMulticastEndpoint swaps the receiver, the old
				// socket is closed which unblocks this ReadFromUDP with a
				// "use of closed network connection" error. This is expected
				// and the next iteration will read from the new socket, so
				// log at Debug rather than Error.
				if errors.Is(err, net.ErrClosed) {
					cfg.Log.Debug().Msg("comms: recv socket swapped; resetting jitter buffer")
					jitter.Reset()
				} else {
					cfg.Log.Error().Err(err).Msg("comms: recv error")
				}

				continue
			}
		}

		if p := rt.localIP.Load(); p != nil {
			if s := *p; s != cachedLocalIPStr {
				cachedLocalIPStr = s
				cachedLocalIP = net.ParseIP(s)
			}
		}

		loopbackDrop := !cfg.Loopback && (src.IP.IsLoopback() || src.IP.Equal(cachedLocalIP))

		if loopbackDrop {
			if cfg.Trace {
				cfg.Log.Trace().Str("src", src.String()).Msg("comms: dropping own packet")
			}

			continue
		}

		// Parse using pion/rtp for proper header validation.
		pkt, parseErr := ParseIncomingRTP(buf[:n])
		if parseErr != nil {
			cfg.Log.Debug().Err(parseErr).Int("bytes", n).Msg("comms: dropping non-RTP datagram")

			continue
		}

		if cfg.Trace {
			cfg.Log.Trace().
				Str("src", src.String()).
				Uint16("seq", pkt.Header.SequenceNumber).
				Uint32("ts", pkt.Header.Timestamp).
				Uint32("ssrc", pkt.Header.SSRC).
				Int("payload_bytes", len(pkt.Payload)).
				Msg("comms: RTP packet received")
		}

		// Skip payload delivery when receive is disabled at runtime.
		if !pc.receiveEnabled.Load() {
			continue
		}

		// Record the arrival time for half-duplex enforcement.
		pc.rxGate.mark()

		// Pass the pion payload directly to the jitter buffer; push()
		// performs its own defensive copy so a separate copy here is
		// unnecessary — the receive buffer (buf) is reused on the next
		// iteration anyway. The SSRC is tracked so that a new talker
		// joining the multicast group does not get silently dropped
		// because their starting sequence number happens to lie in the
		// "past half" of the previous talker's frozen cursor.
		if !jitter.PushWithSSRC(pkt.SSRC, pkt.SequenceNumber, pkt.Payload, func(oldSSRC, newSSRC uint32) {
			cfg.Log.Info().
				Uint32("old_ssrc", oldSSRC).
				Uint32("new_ssrc", newSSRC).
				Msg("comms: RTP SSRC changed; jitter buffer reset")
		}) {
			if n := jitter.Overflows.Load(); n > 0 && n%50 == 0 {
				cfg.Log.Warn().Int64("total_overflows", n).Msg("comms: jitter buffer overflow")
			}
		}
	}
}

// playoutOneFrame produces exactly one frame of PCM audio into out. It is
// the per-tick playout primitive: in production it is invoked from the
// PortAudio output callback once per audio period (one call per ~20 ms), and
// in tests it can be driven directly with a synthetic []float32 buffer.
//
// Driving playout from the consumer (the audio hardware clock) eliminates
// the producer/consumer clock mismatch that the previous time.Ticker-based
// playoutLoop suffered from, and removes the playback buffer entirely as a
// carrier for decoded audio.
//
// Half-duplex: on send-capable ports the function returns silence while the
// node is broadcasting, to prevent local echo. Receive-only ports always
// play back. Web mode is irrelevant here because the PortAudio callback is
// not opened in web mode at all (web RX uses webPlayoutLoop instead).
//
// State: pc.consecutivePLC is owned exclusively by the callback closure for
// this port. Each port has its own PortAudio output stream running on its own
// audio thread, so the field is single-writer. Tests must respect this by
// not invoking playoutOneFrame concurrently with the production callback.
func (cfg *CommsConfig) playoutOneFrame(pc *portChannel, rt *CommsRuntime, jitter *RTPJitterBuffer, out []int16) {
	// Half-duplex: emit silence while broadcasting on a send-capable port.
	if cfg.isBroadcasting(rt) && pc.sendEnabled.Load() {
		zeroInt16(out)

		return
	}

	if !pc.receiveEnabled.Load() {
		zeroInt16(out)

		return
	}

	if jitter == nil {
		zeroInt16(out)

		return
	}

	payload, conceal := jitter.PopOrConceal(concealRecentWindow)
	if payload != nil {
		n, err := rt.decoder.DecodeS16(payload, out)
		jitter.ReleasePayload(payload)

		if err != nil {
			cfg.Log.Debug().Err(err).Msg("comms: opus decode error; falling back to PLC")

			// Try PLC into the same buffer.
			n, err = rt.decoder.DecodeS16(nil, out)
			if err != nil || n != len(out) {
				zeroInt16(out)
				pc.playbackUnderruns.Add(1)

				return
			}

			pc.consecutivePLC = 0

			return
		}

		if n != len(out) {
			for i := n; i < len(out); i++ {
				out[i] = 0
			}
		}

		if cfg.Trace {
			cfg.Log.Trace().Msgf("comms: decoded %d samples", n)
		}

		pc.consecutivePLC = 0

		return
	}

	if conceal {
		pc.consecutivePLC++
		if pc.consecutivePLC <= maxConsecutivePLC {
			if cfg.Trace {
				cfg.Log.Trace().Int("consecutive_plc", pc.consecutivePLC).Msg("comms: jitter buffer gap → PLC")
			}

			n, err := rt.decoder.DecodeS16(nil, out)
			if err != nil || n != len(out) {
				zeroInt16(out)
			}

			return
		}

		// Sustained loss: emit clean silence rather than degraded PLC.
		zeroInt16(out)

		return
	}

	// Buffer empty and stream not active → genuine silence (no underrun).
	zeroInt16(out)
}

// webPlayoutLoop is the receive-side consumer used in web mode (rt.webBridge
// non-nil). PortAudio is not active in web mode, so the loop runs on a 20 ms
// software ticker and forwards raw Opus payloads to the WebAudioBridge for
// streaming to the browser. PLC, half-duplex enforcement, and decoding all
// happen on the browser side and are skipped here.
func (cfg *CommsConfig) webPlayoutLoop(ctx context.Context, jitter *RTPJitterBuffer, rt *CommsRuntime) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		payload, _ := jitter.PopOrConceal(concealRecentWindow)
		if payload == nil {
			continue
		}

		cp := make([]byte, len(payload))
		copy(cp, payload)
		rt.webBridge.PushRxFrame(cp)
		jitter.ReleasePayload(payload)
	}
}

// Ensure net is used (ReadFromUDP returns *net.UDPAddr).
var _ *net.UDPAddr
