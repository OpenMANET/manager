package comms

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

// rxActiveThreshold is the window after the last received remote RTP packet
// during which the channel is considered "actively receiving". Transmission
// is blocked while receiving is active (half-duplex enforcement).
const rxActiveThreshold time.Duration = 400 * time.Millisecond

// isReceivingRemote returns true when a valid RTP packet was received from a
// remote peer within the last rxActiveThreshold on any send-enabled port.
// This is the receive-side component of half-duplex: transmission must not
// begin while a shared send+receive channel is actively carrying incoming audio.
func (cfg *CommsConfig) isReceivingRemote(rt *CommsRuntime) bool {
	for _, pc := range rt.ports {
		if !pc.sendEnabled.Load() {
			continue
		}

		last := pc.lastRemoteRx.Load()
		if last != 0 && time.Since(time.Unix(0, last)) < rxActiveThreshold {
			return true
		}
	}

	return false
}

// ─── Receive path ─────────────────────────────────────────────────────────────

// receiveLoop reads datagrams from pc.receiver, parses them as RTP packets
// using pion/rtp, and pushes the payloads into the jitter buffer. A companion
// playoutLoop goroutine drains the jitter buffer at 20 ms intervals.
//
// The loop discards packets from our own IP (loopback prevention) unless
// cfg.Loopback is true, and skips queuing frames when pc.receiveEnabled is false.
func (cfg *CommsConfig) receiveLoop(ctx context.Context, pc *portChannel, rt *CommsRuntime) { //nolint:gocognit
	buf := make([]byte, 1500)
	jitter := newRTPJitterBuffer(jitterPrebufferPackets, jitterMaxDepth)

	go cfg.playoutLoop(ctx, jitter, pc, rt)

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
					jitter.reset()
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

		// Skip payload delivery when receive is disabled at runtime.
		if !pc.receiveEnabled.Load() {
			continue
		}

		// Record the arrival time for half-duplex enforcement.
		pc.lastRemoteRx.Store(time.Now().UnixNano())

		// Pass the pion payload directly to the jitter buffer; push()
		// performs its own defensive copy so a separate copy here is
		// unnecessary — the receive buffer (buf) is reused on the next
		// iteration anyway. The SSRC is tracked so that a new talker
		// joining the multicast group does not get silently dropped
		// because their starting sequence number happens to lie in the
		// "past half" of the previous talker's frozen cursor.
		if !jitter.pushWithSSRC(pkt.SSRC, pkt.SequenceNumber, pkt.Payload, func(oldSSRC, newSSRC uint32) {
			cfg.Log.Info().
				Uint32("old_ssrc", oldSSRC).
				Uint32("new_ssrc", newSSRC).
				Msg("comms: RTP SSRC changed; jitter buffer reset")
		}) {
			if n := jitter.overflows.Load(); n > 0 && n%50 == 0 {
				cfg.Log.Warn().Int64("total_overflows", n).Msg("comms: jitter buffer overflow")
			}
		}
	}
}

// playoutLoop drives the RTP jitter buffer at a 20 ms tick rate.
// It pops ready frames, applies PLC for missing frames, and queues decoded
// PCM to pc.playbackBuffer. Exits when ctx is canceled.
//
// Half-duplex: on send-capable ports playback is suppressed while broadcasting
// to prevent local echo. Receive-only ports always play back.
//
// Backpressure: when the playback buffer is ≥75% full the tick is skipped,
// allowing the PortAudio callback (hardware clock) to drain before more
// frames are produced. The jitter buffer absorbs the delay.
func (cfg *CommsConfig) playoutLoop(ctx context.Context, jitter *rtpJitterBuffer, pc *portChannel, rt *CommsRuntime) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	// Use the pre-computed high-water mark when available; fall back to
	// computing it here for portChannels built outside of buildAudio (tests).
	hwm := pc.playbackHighWaterMark
	if hwm == 0 {
		hwm = cap(pc.playbackBuffer) * 3 / 4
	}

	// Track consecutive PLC frames to cap robotic artifacts during burst
	// loss. After maxConsecutivePLC frames (100 ms on a mesh), silence is
	// emitted instead of increasingly degraded PLC output.
	var consecutivePLC int

	const maxConsecutivePLC = 5

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// Half-duplex: suppress playback on send-capable ports while broadcasting.
		// In web mode the browser manages its own audio I/O and echo
		// cancellation, so Rx must continue flowing during TX.
		if rt.webBridge == nil && cfg.isBroadcasting(rt) && pc.sendEnabled.Load() {
			continue
		}

		// Skip if receive has been disabled at runtime.
		if !pc.receiveEnabled.Load() {
			continue
		}

		// Web mode: forward raw Opus to the web client instead of
		// decoding to PCM and queueing to the PortAudio playback buffer.
		// Backpressure and PLC are skipped because PortAudio is not active
		// and Opus PLC produces PCM which cannot be forwarded as-is.
		if rt.webBridge != nil {
			payload, _ := jitter.popOrConceal(100 * time.Millisecond)
			if payload != nil {
				cp := make([]byte, len(payload))
				copy(cp, payload)
				rt.webBridge.PushRxFrame(cp)
				jitter.releasePayload(payload)
			}

			continue
		}

		// Backpressure: drain the oldest frame(s) when the playback
		// channel is nearly full rather than skipping this tick. This
		// keeps the 20 ms playout cadence intact and prevents the
		// jitter buffer from drifting out of sync with the hardware
		// clock.
		for len(pc.playbackBuffer) >= hwm {
			select {
			case old := <-pc.playbackBuffer:
				returnFloat32(old)
			default:
			}
		}

		payload, conceal := jitter.popOrConceal(100 * time.Millisecond)
		if payload != nil {
			cfg.decodeAndQueue(pc, rt, payload)
			jitter.releasePayload(payload)

			consecutivePLC = 0

			continue
		}

		if conceal {
			consecutivePLC++
			cfg.emitConcealFrame(pc, rt, consecutivePLC, maxConsecutivePLC)
		}
	}
}

// emitConcealFrame emits either a PLC or silence frame depending on how many
// consecutive concealment frames have been produced. This caps robotic PLC
// artifacts during burst loss on the mesh.
func (cfg *CommsConfig) emitConcealFrame(pc *portChannel, rt *CommsRuntime, consecutive, max int) {
	if consecutive <= max {
		if cfg.Trace {
			cfg.Log.Trace().Msg("comms: jitter buffer gap → PLC")
		}

		cfg.decodeAndQueuePLC(pc, rt)
	} else {
		cfg.decodeAndQueueSilence(pc)
	}
}

// decodeAndQueue decodes an Opus payload directly into a pooled float32 PCM
// buffer and queues it to pc.playbackBuffer. Frames are silently dropped when
// the buffer is full. The caller must call jitter.releasePayload on the payload
// after this function returns.
func (cfg *CommsConfig) decodeAndQueue(pc *portChannel, rt *CommsRuntime, payload []byte) {
	outPtr := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
	out := *outPtr

	n, err := rt.decoder.DecodeFloat32(payload, out)
	if err != nil {
		cfg.Log.Debug().Err(err).Msg("comms: opus decode error; falling back to PLC")

		n, err = rt.decoder.DecodeFloat32(nil, out)
		if err != nil || n <= 0 {
			returnFloat32(out)

			return
		}
	}

	out = out[:n]

	select {
	case pc.playbackBuffer <- out:
		if cfg.Trace {
			cfg.Log.Trace().Msgf("comms: queued %d samples (depth=%d)", n, len(pc.playbackBuffer))
		}
	default:
		returnFloat32(out)
		logPlaybackDrop(&pc.playbackDrops, cfg, "comms: playback buffer full; dropping packet")
	}
}

// decodeAndQueuePLC generates a Packet Loss Concealment frame via the Opus
// decoder (nil payload) and queues it to pc.playbackBuffer.
func (cfg *CommsConfig) decodeAndQueuePLC(pc *portChannel, rt *CommsRuntime) {
	outPtr := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
	out := *outPtr

	n, err := rt.decoder.DecodeFloat32(nil, out)
	if err != nil || n <= 0 {
		returnFloat32(out)

		return
	}

	out = out[:n]

	select {
	case pc.playbackBuffer <- out:
		if cfg.Trace {
			cfg.Log.Trace().Msgf("comms: queued PLC %d samples (depth=%d)", n, len(pc.playbackBuffer))
		}
	default:
		returnFloat32(out)
		logPlaybackDrop(&pc.playbackDrops, cfg, "comms: playback buffer full; dropping PLC frame")
	}
}

// decodeAndQueueSilence queues a zeroed float32 frame to pc.playbackBuffer.
// Used after maxConsecutivePLC frames to replace degraded PLC output with
// clean silence during sustained burst loss.
func (cfg *CommsConfig) decodeAndQueueSilence(pc *portChannel) {
	outPtr := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
	out := (*outPtr)[:frameSize]

	for i := range out {
		out[i] = 0
	}

	select {
	case pc.playbackBuffer <- out:
	default:
		returnFloat32(out)
	}
}

// playbackDropLogInterval controls how often repeated playback-buffer-full
// warnings are emitted. The first drop always logs; subsequent drops log
// once every playbackDropLogInterval occurrences.
const playbackDropLogInterval = 50

// logPlaybackDrop increments the drop counter and emits a Warn log on the
// first drop and then every playbackDropLogInterval drops thereafter. All
// other drops are recorded at Debug level so the counter remains accurate
// without flooding logs.
func logPlaybackDrop(counter *atomic.Int64, cfg *CommsConfig, msg string) {
	n := counter.Add(1)
	if n == 1 || n%playbackDropLogInterval == 0 {
		cfg.Log.Warn().Int64("total_drops", n).Msg(msg)
	} else if cfg.Debug {
		cfg.Log.Debug().Int64("total_drops", n).Msg(msg)
	}
}

// Ensure net is used (ReadFromUDP returns *net.UDPAddr).
var _ *net.UDPAddr
