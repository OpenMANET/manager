package ptt

import (
	"net"
	"strconv"
	"sync"
	"time"

	evdev "github.com/gvalkov/golang-evdev"
)

const (
	rtpJitterPrebufferPackets = 3
	rtpJitterMaxDepth         = 24
)

type rtpJitterBuffer struct {
	mu        sync.Mutex
	frames    map[uint16][]byte
	expected  uint16
	init      bool
	started   bool
	prebuffer int
	maxDepth  int
	lastPush  time.Time
}

func newRTPJitterBuffer(prebuffer, maxDepth int) *rtpJitterBuffer {
	return &rtpJitterBuffer{
		frames:    make(map[uint16][]byte),
		prebuffer: prebuffer,
		maxDepth:  maxDepth,
	}
}

func seqLess(a, b uint16) bool {
	return int16(a-b) < 0
}

func (jb *rtpJitterBuffer) push(seq uint16, payload []byte) bool {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if !jb.init {
		jb.expected = seq
		jb.init = true
	}

	// Drop packets that are older than the current playout cursor.
	if seqLess(seq, jb.expected) {
		return false
	}

	if _, exists := jb.frames[seq]; exists {
		return false
	}

	if len(jb.frames) >= jb.maxDepth {
		return false
	}

	copied := make([]byte, len(payload))
	copy(copied, payload)
	jb.frames[seq] = copied
	jb.lastPush = time.Now()
	return true
}

func (jb *rtpJitterBuffer) popReady() (payload []byte, ready bool, skippedMissing bool) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if !jb.init {
		return nil, false, false
	}

	if !jb.started {
		if len(jb.frames) < jb.prebuffer {
			return nil, false, false
		}
		jb.started = true
	}

	if payload, ok := jb.frames[jb.expected]; ok {
		delete(jb.frames, jb.expected)
		jb.expected++
		return payload, true, false
	}

	// If we've buffered a lot and still don't have the expected packet, skip it.
	if len(jb.frames) >= jb.maxDepth/2 {
		jb.expected++
		return nil, false, true
	}

	return nil, false, false
}

func (jb *rtpJitterBuffer) shouldConceal(recentWindow time.Duration) bool {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if !jb.started || jb.lastPush.IsZero() {
		return false
	}

	return time.Since(jb.lastPush) <= recentWindow
}

func (ptt *PTTConfig) rtpPlayoutLoop(jitter *rtpJitterBuffer) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		payload, ready, skipped := jitter.popReady()
		if skipped {
			if ptt.runtime.traceEnabled {
				ptt.Log.Trace().Msg("RTP jitter buffer skipped missing packet")
			}
			ptt.decodeAndQueuePLC()
			continue
		}

		if ready {
			ptt.decodeAndQueue(payload)
			continue
		}

		if jitter.shouldConceal(100 * time.Millisecond) {
			ptt.decodeAndQueuePLC()
		}
	}
}

func (ptt *PTTConfig) receiveLoop(udpConn *net.UDPConn) {
	buf := make([]byte, 1500)
	jitter := newRTPJitterBuffer(rtpJitterPrebufferPackets, rtpJitterMaxDepth)
	if ptt.runtime.protocol == protocolRTP {
		go ptt.rtpPlayoutLoop(jitter)
	}
	for {
		n, src, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			ptt.Log.Error().Err(err).Msg("Recv error")
			continue
		}

		loopbackDrop := !ptt.runtime.loopbackAudio && (src.IP.IsLoopback() || src.IP.String() == ptt.runtime.localIP)
		if ptt.runtime.traceEnabled {
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

		if ptt.runtime.protocol == protocolRTP {
			seq, _, _, ok := parseRTPHeader(frame)
			if !ok {
				ptt.Log.Debug().Msg("Dropping packet: invalid RTP header")
				continue
			}
			payload, _ := unwrapRTP(frame)
			if pushed := jitter.push(seq, payload); !pushed {
				continue
			}
			continue
		}

		if payload, ok := unwrapRTP(frame); ok {
			frame = payload
		}
		ptt.decodeAndQueue(frame)
	}
}

func (ptt *PTTConfig) decodeAndQueue(frame []byte) {
	pcm := make([]int16, frameSize)
	n, err := ptt.runtime.decoder.Decode(frame, pcm)
	if err != nil {
		return
	}

	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(pcm[i]) / 32768
	}

	select {
	case ptt.runtime.playbackBuffer <- out:
		if ptt.runtime.traceEnabled {
			ptt.Log.Trace().Msgf("Queued playback buffer with %d samples (depth=%d)", len(out), len(ptt.runtime.playbackBuffer))
		}
	default:
		ptt.Log.Warn().Msg("⚠️ Playback buffer full! Dropping packet.")
	}
}

func (ptt *PTTConfig) decodeAndQueuePLC() {
	pcm := make([]int16, frameSize)
	n, err := ptt.runtime.decoder.Decode(nil, pcm)
	if err != nil || n <= 0 {
		return
	}

	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = float32(pcm[i]) / 32768
	}

	select {
	case ptt.runtime.playbackBuffer <- out:
		if ptt.runtime.traceEnabled {
			ptt.Log.Trace().Msgf("Queued PLC playback buffer with %d samples (depth=%d)", len(out), len(ptt.runtime.playbackBuffer))
		}
	default:
		ptt.Log.Warn().Msg("⚠️ Playback buffer full! Dropping PLC frame.")
	}
}

func (ptt *PTTConfig) monitorPTT(dev *evdev.InputDevice) {
	for {
		ev, err := dev.ReadOne()
		if err != nil {
			continue
		}
		if ev.Type != evdev.EV_KEY {
			continue
		}
		match := false
		if ptt.PttKey == "any" {
			match = true
		} else if kc, err := strconv.Atoi(ptt.PttKey); err == nil && kc >= 0 && kc <= 65535 && ev.Code == uint16(kc) {
			match = true
		}
		if !match {
			continue
		}

		switch ev.Value {
		case 1:
			ptt.Log.Debug().Msgf("PTT down (code=%d)", ev.Code)
			if ptt.isBroadcasting() {
				ptt.Log.Debug().Msgf("PTT toggle: stopping transmission")
				ptt.endTransmission()
			} else {
				ptt.Log.Debug().Msgf("PTT toggle: starting transmission")
				ptt.beginTransmission()
			}
		case 0:
			ptt.Log.Debug().Msgf("PTT up (code=%d)", ev.Code)
		}
	}
}

func (ptt *PTTConfig) isBroadcasting() bool {
	ptt.runtime.recordMutex.Lock()
	defer ptt.runtime.recordMutex.Unlock()
	return ptt.runtime.broadcasting
}

func (ptt *PTTConfig) drainPlaybackBuffer() {
	for {
		select {
		case <-ptt.runtime.playbackBuffer:
		default:
			return
		}
	}
}

func (ptt *PTTConfig) beginTransmission() {
	ptt.runtime.recordMutex.Lock()
	if ptt.runtime.broadcasting {
		ptt.Log.Debug().Msgf("PTT down ignored; already broadcasting")
		ptt.runtime.recordMutex.Unlock()
		return
	}
	ptt.runtime.broadcasting = true
	ptt.runtime.recordMutex.Unlock()

	ptt.Log.Debug().Msgf("Begin transmission: playing start tone and starting mic stream")
	ptt.drainPlaybackBuffer()
	ptt.runtime.playbackBuffer <- ptt.runtime.beepBufferStart
	time.Sleep(200 * time.Millisecond)

	if ptt.runtime.broadcastStream == nil {
		ptt.Log.Warn().Msg("Mic stream is nil; attempting to reopen")
		if err := ptt.reopenBroadcastStream(); err != nil {
			ptt.Log.Error().Err(err).Msg("Failed to reopen mic stream")
			ptt.runtime.recordMutex.Lock()
			ptt.runtime.broadcasting = false
			ptt.runtime.recordMutex.Unlock()
			return
		}
	}

	if err := ptt.runtime.broadcastStream.Start(); err != nil {
		ptt.Log.Error().Err(err).Msg("Failed to start mic stream; attempting to reopen stream")
		if reErr := ptt.reopenBroadcastStream(); reErr != nil {
			ptt.Log.Error().Err(reErr).Msg("Failed to reopen mic stream")
			ptt.runtime.recordMutex.Lock()
			ptt.runtime.broadcasting = false
			ptt.runtime.recordMutex.Unlock()
			return
		}
		if err := ptt.runtime.broadcastStream.Start(); err != nil {
			ptt.Log.Error().Err(err).Msg("Failed to start mic stream after reopen")
			ptt.runtime.recordMutex.Lock()
			ptt.runtime.broadcasting = false
			ptt.runtime.recordMutex.Unlock()
			return
		}
	}

	ptt.Log.Debug().Msg("Mic stream started")
}

func (ptt *PTTConfig) endTransmission() {
	ptt.runtime.recordMutex.Lock()

	if !ptt.runtime.broadcasting {
		ptt.Log.Debug().Msgf("PTT up ignored; mic already idle")
		ptt.runtime.recordMutex.Unlock()
		return
	}

	ptt.runtime.recordMutex.Unlock()

	ptt.Log.Debug().Msg("End transmission: stopping mic stream and playing stop tone")
	if ptt.runtime.broadcastStream == nil {
		ptt.Log.Warn().Msg("Mic stream was nil during stop")
	} else if err := ptt.runtime.broadcastStream.Stop(); err != nil {
		ptt.Log.Error().Err(err).Msg("stop mic")
	} else {
		ptt.Log.Debug().Msg("Mic stream stopped")
	}

	ptt.drainPlaybackBuffer()
	ptt.runtime.playbackBuffer <- ptt.runtime.beepBufferStop

	ptt.runtime.recordMutex.Lock()
	ptt.runtime.broadcasting = false
	ptt.runtime.recordMutex.Unlock()
}
