//go:build !omd_omit_comms

package comms

import (
	"context"
	"net"
	"time"
)

// ─── Receive path ─────────────────────────────────────────────────────────────

// receiveLoop reads datagrams from rt.receiver, parses them as RTP packets
// using pion/rtp, and pushes the payloads into the jitter buffer. A companion
// playoutLoop goroutine drains the jitter buffer at 20 ms intervals.
//
// The loop discards packets from our own IP (loopback prevention) unless
// cfg.Loopback is true.
func (cfg *CommsConfig) receiveLoop(ctx context.Context, rt *CommsRuntime) {
	buf := make([]byte, 1500)
	jitter := newRTPJitterBuffer(jitterPrebufferPackets, jitterMaxDepth)

	go cfg.playoutLoop(ctx, jitter, rt)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, src, err := rt.receiver.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				cfg.Log.Error().Err(err).Msg("comms: recv error")

				continue
			}
		}

		var localIP string
		if v := rt.localIP.Load(); v != nil {
			if s, ok := v.(string); ok {
				localIP = s
			}
		}

		loopbackDrop := !cfg.Loopback && (src.IP.IsLoopback() || src.IP.String() == localIP)

		if loopbackDrop {
			if cfg.Trace {
				cfg.Log.Trace().Str("src", src.String()).Msg("comms: dropping own packet")
			}

			continue
		}

		// Parse using pion/rtp for proper header validation.
		pkt, parseErr := parseIncomingRTP(buf[:n])
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

		// Copy payload before releasing buf to the next read.
		payload := make([]byte, len(pkt.Payload))
		copy(payload, pkt.Payload)

		jitter.push(pkt.SequenceNumber, payload)
	}
}

// playoutLoop drives the RTP jitter buffer at a 20 ms tick rate.
// It pops ready frames, applies PLC for missing frames, and queues decoded
// PCM to rt.playbackBuffer. Exits when ctx is canceled.
func (cfg *CommsConfig) playoutLoop(ctx context.Context, jitter *rtpJitterBuffer, rt *CommsRuntime) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if cfg.isBroadcasting(rt) {
			continue
		}

		payload, ready, skipped := jitter.popReady()
		if skipped {
			if cfg.Trace {
				cfg.Log.Trace().Msg("comms: jitter buffer skipped missing packet → PLC")
			}

			cfg.decodeAndQueuePLC(rt)

			continue
		}

		if ready {
			cfg.decodeAndQueue(rt, payload)

			continue
		}

		if jitter.shouldConceal(100 * time.Millisecond) {
			// Advance the playout cursor before generating PLC so the late
			// original (if it arrives) is treated as stale.
			jitter.advancePast()
			cfg.decodeAndQueuePLC(rt)
		}
	}
}

// decodeAndQueue decodes an Opus payload into float32 PCM and queues it to
// rt.playbackBuffer. Frames are silently dropped when the buffer is full.
func (cfg *CommsConfig) decodeAndQueue(rt *CommsRuntime, payload []byte) {
	pcm := make([]int16, frameSize)

	n, err := rt.decoder.Decode(payload, pcm)
	if err != nil {
		cfg.Log.Debug().Err(err).Msg("comms: opus decode error")

		return
	}

	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(pcm[i]) / 32768
	}

	select {
	case rt.playbackBuffer <- out:
		if cfg.Trace {
			cfg.Log.Trace().Msgf("comms: queued %d samples (depth=%d)", n, len(rt.playbackBuffer))
		}
	default:
		cfg.Log.Warn().Msg("comms: playback buffer full; dropping packet")
	}
}

// decodeAndQueuePLC generates a Packet Loss Concealment frame via the Opus
// decoder (nil data) and queues it to rt.playbackBuffer.
func (cfg *CommsConfig) decodeAndQueuePLC(rt *CommsRuntime) {
	pcm := make([]int16, frameSize)

	n, err := rt.decoder.Decode(nil, pcm)
	if err != nil || n <= 0 {
		return
	}

	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(pcm[i]) / 32768
	}

	select {
	case rt.playbackBuffer <- out:
		if cfg.Trace {
			cfg.Log.Trace().Msgf("comms: queued PLC %d samples (depth=%d)", n, len(rt.playbackBuffer))
		}
	default:
		cfg.Log.Warn().Msg("comms: playback buffer full; dropping PLC frame")
	}
}

// Ensure net is used (ReadFromUDP returns *net.UDPAddr).
var _ *net.UDPAddr
