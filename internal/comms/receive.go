package comms

import (
	"context"
	"net"
	"time"
)

// rxActiveThreshold is the window after the last received remote RTP packet
// during which the channel is considered "actively receiving". Transmission
// is blocked while receiving is active (half-duplex enforcement).
const rxActiveThreshold time.Duration = 400 * time.Millisecond

// isReceivingRemote returns true when a valid RTP packet was received from a
// remote peer within the last rxActiveThreshold. This is the receive-side
// component of half-duplex: transmission must not begin while the channel is
// actively carrying incoming audio.
func (cfg *CommsConfig) isReceivingRemote(rt *CommsRuntime) bool {
	last := rt.lastRemoteRx.Load()
	if last == 0 {
		return false
	}

	return time.Since(time.Unix(0, last)) < rxActiveThreshold
}

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

		// Record the arrival time for half-duplex enforcement.
		rt.lastRemoteRx.Store(time.Now().UnixNano())

		// Pass the pion payload directly to the jitter buffer; push()
		// performs its own defensive copy so a separate copy here is
		// unnecessary — the receive buffer (buf) is reused on the next
		// iteration anyway.
		jitter.push(pkt.SequenceNumber, pkt.Payload)
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

		payload, conceal := jitter.popOrConceal(100 * time.Millisecond)
		if payload != nil {
			cfg.decodeAndQueue(rt, payload)

			continue
		}

		if conceal {
			if cfg.Trace {
				cfg.Log.Trace().Msg("comms: jitter buffer gap → PLC")
			}

			cfg.decodeAndQueuePLC(rt)
		}
	}
}

// decodeAndQueue decodes an Opus payload into float32 PCM and queues it to
// rt.playbackBuffer. Frames are silently dropped when the buffer is full.
func (cfg *CommsConfig) decodeAndQueue(rt *CommsRuntime, payload []byte) {
	pcmPtr := int16Pool.Get().(*[]int16) //nolint:forcetypeassert
	pcm := *pcmPtr

	n, err := rt.decoder.Decode(payload, pcm)
	if err != nil {
		int16Pool.Put(pcmPtr)
		cfg.Log.Debug().Err(err).Msg("comms: opus decode error")

		return
	}

	outPtr := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
	out := (*outPtr)[:n]

	for i := 0; i < n; i++ {
		out[i] = float32(pcm[i]) / 32768
	}

	int16Pool.Put(pcmPtr)

	select {
	case rt.playbackBuffer <- out:
		if cfg.Trace {
			cfg.Log.Trace().Msgf("comms: queued %d samples (depth=%d)", n, len(rt.playbackBuffer))
		}
	default:
		returnFloat32(out)
		cfg.Log.Warn().Msg("comms: playback buffer full; dropping packet")
	}
}

// decodeAndQueuePLC generates a Packet Loss Concealment frame via the Opus
// decoder (nil data) and queues it to rt.playbackBuffer.
func (cfg *CommsConfig) decodeAndQueuePLC(rt *CommsRuntime) {
	pcmPtr := int16Pool.Get().(*[]int16) //nolint:forcetypeassert
	pcm := *pcmPtr                       //nolint:forcetypeassert

	n, err := rt.decoder.Decode(nil, pcm)
	if err != nil || n <= 0 {
		int16Pool.Put(pcmPtr)

		return
	}

	outPtr := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
	out := (*outPtr)[:n]

	for i := 0; i < n; i++ {
		out[i] = float32(pcm[i]) / 32768
	}

	int16Pool.Put(pcmPtr)

	select {
	case rt.playbackBuffer <- out:
		if cfg.Trace {
			cfg.Log.Trace().Msgf("comms: queued PLC %d samples (depth=%d)", n, len(rt.playbackBuffer))
		}
	default:
		returnFloat32(out)
		cfg.Log.Warn().Msg("comms: playback buffer full; dropping PLC frame")
	}
}

// Ensure net is used (ReadFromUDP returns *net.UDPAddr).
var _ *net.UDPAddr
