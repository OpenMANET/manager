package ptt

import (
	"context"
	"net"
	"time"
)

// ─── Receive path ─────────────────────────────────────────────────────────────

// receiveLoop reads datagrams from rt.receiver, manages the RTP jitter buffer
// (when protocol is "rtp"), and queues decoded PCM frames to rt.playbackBuffer.
// It exits when ctx is canceled or the receiver returns an error after ctx is done.
func (ptt *PTTConfig) receiveLoop(ctx context.Context, rt *PTTRuntime) { //nolint:gocognit
	buf := make([]byte, 1500)
	jitter := newRTPJitterBuffer(rtpJitterPrebufferPackets, rtpJitterMaxDepth)

	if ptt.Protocol == protocolRTP {
		go ptt.rtpPlayoutLoop(ctx, jitter, rt)
	}

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
				ptt.Log.Error().Err(err).Msg("Recv error")

				continue
			}
		}

		var localIP string
		if v := rt.localIP.Load(); v != nil {
			if s, ok := v.(string); ok {
				localIP = s
			}
		}

		loopbackDrop := !ptt.Loopback && (src.IP.IsLoopback() || src.IP.String() == localIP)

		if ptt.Trace {
			if seq, ts, ssrc, ok := parseRTPHeader(buf[:n]); ok {
				ptt.Log.Trace().
					Str("src", src.String()).
					Int("bytes", n).
					Str("protocol", "rtp").
					Uint16("rtp_seq", seq).
					Uint32("rtp_ts", ts).
					Uint32("rtp_ssrc", ssrc).
					Bool("loopback_dropped", loopbackDrop).
					Msg("PTT multicast packet received")
			} else {
				ptt.Log.Trace().
					Str("src", src.String()).
					Int("bytes", n).
					Str("protocol", "udp").
					Bool("loopback_dropped", loopbackDrop).
					Msg("PTT multicast packet received")
			}
		}

		if loopbackDrop {
			continue
		}

		frame := make([]byte, n)
		copy(frame, buf[:n])

		if ptt.Protocol == protocolRTP {
			seq, _, _, ok := parseRTPHeader(frame)
			if !ok {
				ptt.Log.Debug().Msg("Dropping packet: invalid RTP header")

				continue
			}

			payload, _ := unwrapRTP(frame)
			if !jitter.push(seq, payload) {
				continue
			}

			continue
		}

		// UDP mode: auto-detect and unwrap RTP if present.
		if payload, ok := unwrapRTP(frame); ok {
			frame = payload
		}

		if ptt.isBroadcasting(rt) {
			continue
		}

		ptt.decodeAndQueue(rt, frame)
	}
}

// rtpPlayoutLoop drives the RTP jitter buffer at a 20 ms tick rate.
// It pops ready frames, applies PLC for missing frames, and queues to
// rt.playbackBuffer.  Exits when ctx is canceled.
func (ptt *PTTConfig) rtpPlayoutLoop(ctx context.Context, jitter *rtpJitterBuffer, rt *PTTRuntime) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if ptt.isBroadcasting(rt) {
			continue
		}

		payload, ready, skipped := jitter.popReady()
		if skipped {
			if ptt.Trace {
				ptt.Log.Trace().Msg("RTP jitter buffer skipped missing packet")
			}

			ptt.decodeAndQueuePLC(rt)

			continue
		}

		if ready {
			ptt.decodeAndQueue(rt, payload)

			continue
		}

		if jitter.shouldConceal(100 * time.Millisecond) {
			ptt.decodeAndQueuePLC(rt)
		}
	}
}

// decodeAndQueue decodes an Opus frame into float32 PCM and queues it to
// rt.playbackBuffer.  Drops the frame with a warning if the buffer is full.
func (ptt *PTTConfig) decodeAndQueue(rt *PTTRuntime, frame []byte) {
	pcm := make([]int16, frameSize)

	n, err := rt.decoder.Decode(frame, pcm)
	if err != nil {
		return
	}

	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(pcm[i]) / 32768
	}

	select {
	case rt.playbackBuffer <- out:
		if ptt.Trace {
			ptt.Log.Trace().Msgf("Queued playback buffer with %d samples (depth=%d)", len(out), len(rt.playbackBuffer))
		}
	default:
		ptt.Log.Warn().Msg("⚠️ Playback buffer full! Dropping packet.")
	}
}

// decodeAndQueuePLC generates a Packet Loss Concealment frame via the Opus
// decoder and queues it to rt.playbackBuffer.
func (ptt *PTTConfig) decodeAndQueuePLC(rt *PTTRuntime) {
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
		if ptt.Trace {
			ptt.Log.Trace().Msgf("Queued PLC playback buffer with %d samples (depth=%d)", len(out), len(rt.playbackBuffer))
		}
	default:
		ptt.Log.Warn().Msg("⚠️ Playback buffer full! Dropping PLC frame.")
	}
}

// ─── Transmission state ───────────────────────────────────────────────────────

func (ptt *PTTConfig) isBroadcasting(rt *PTTRuntime) bool {
	rt.recordMutex.Lock()
	defer rt.recordMutex.Unlock()

	return rt.broadcasting
}

func (ptt *PTTConfig) drainPlaybackBuffer(rt *PTTRuntime) {
	for {
		select {
		case <-rt.playbackBuffer:
		default:
			return
		}
	}
}

// beginTransmission starts the mic capture stream and plays the start-tone
// into the local speaker to signal the start of transmission.
//
// If the broadcast stream is nil or fails to start, rt.reopenBroadcast is
// called to rebuild it using the input device that was resolved at startup.
func (ptt *PTTConfig) beginTransmission(rt *PTTRuntime) {
	rt.recordMutex.Lock()
	if rt.broadcasting {
		ptt.Log.Debug().Msg("PTT down ignored; already broadcasting")
		rt.recordMutex.Unlock()

		return
	}

	rt.broadcasting = true
	rt.recordMutex.Unlock()

	ptt.Log.Debug().Msg("Begin transmission: playing start tone and starting mic stream")
	ptt.drainPlaybackBuffer(rt)

	rt.playbackBuffer <- rt.beepBufferStart

	time.Sleep(200 * time.Millisecond)

	if rt.broadcastStream == nil {
		ptt.Log.Warn().Msg("Mic stream is nil; attempting to reopen")

		if rt.reopenBroadcast != nil {
			if err := rt.reopenBroadcast(); err != nil {
				ptt.Log.Error().Err(err).Msg("Failed to reopen mic stream")
				rt.recordMutex.Lock()
				rt.broadcasting = false
				rt.recordMutex.Unlock()

				return
			}
		}
	}

	if rt.broadcastStream == nil {
		ptt.Log.Error().Msg("Mic stream still nil after reopen attempt")
		rt.recordMutex.Lock()
		rt.broadcasting = false
		rt.recordMutex.Unlock()

		return
	}

	if err := rt.broadcastStream.Start(); err != nil {
		ptt.Log.Error().Err(err).Msg("Failed to start mic stream; attempting to reopen stream")

		if rt.reopenBroadcast != nil {
			if reErr := rt.reopenBroadcast(); reErr != nil {
				ptt.Log.Error().Err(reErr).Msg("Failed to reopen mic stream")
				rt.recordMutex.Lock()
				rt.broadcasting = false
				rt.recordMutex.Unlock()

				return
			}
		}

		if err := rt.broadcastStream.Start(); err != nil {
			ptt.Log.Error().Err(err).Msg("Failed to start mic stream after reopen")
			rt.recordMutex.Lock()
			rt.broadcasting = false
			rt.recordMutex.Unlock()

			return
		}
	}

	ptt.Log.Debug().Msg("Mic stream started")
}

// endTransmission stops the mic capture stream and plays the stop-tone.
func (ptt *PTTConfig) endTransmission(rt *PTTRuntime) {
	rt.recordMutex.Lock()
	if !rt.broadcasting {
		ptt.Log.Debug().Msg("PTT up ignored; mic already idle")
		rt.recordMutex.Unlock()

		return
	}
	rt.recordMutex.Unlock()

	ptt.Log.Debug().Msg("End transmission: stopping mic stream and playing stop tone")

	if rt.broadcastStream == nil {
		ptt.Log.Warn().Msg("Mic stream was nil during stop")
	} else if err := rt.broadcastStream.Stop(); err != nil {
		ptt.Log.Error().Err(err).Msg("stop mic")
	} else {
		ptt.Log.Debug().Msg("Mic stream stopped")
	}

	ptt.drainPlaybackBuffer(rt)

	rt.playbackBuffer <- rt.beepBufferStop

	rt.recordMutex.Lock()
	rt.broadcasting = false
	rt.recordMutex.Unlock()
}

// Ensure net is used (ReadFromUDP returns *net.UDPAddr).
var _ *net.UDPAddr
