package ptt

import (
	"net"
	"strconv"
	"time"

	"github.com/gordonklaus/portaudio"
	evdev "github.com/gvalkov/golang-evdev"
)

func (ptt *PTTConfig) receiveLoop(udpConn *net.UDPConn) {
	buf := make([]byte, 1500)
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

		ptt.Log.Debug().Msgf("Received %d bytes from %s", n, src.IP.String())
		if loopbackDrop {
			continue
		}

		frame := make([]byte, n)
		copy(frame, buf[:n])

		if payload, ok := unwrapRTP(frame); ok {
			frame = payload
		} else if ptt.runtime.protocol == protocolRTP {
			ptt.Log.Debug().Msg("Dropping packet: invalid RTP header")
			continue
		}

		pcm := make([]int16, frameSize)
		n, err = ptt.runtime.decoder.Decode(frame, pcm)
		if err != nil {
			continue
		}
		out := make([]float32, n)
		for i := 0; i < n; i++ {
			out[i] = float32(pcm[i]) / 32768
		}

		select {
		case ptt.runtime.playbackBuffer <- out:
			ptt.Log.Debug().Msgf("Queued playback buffer with %d samples (depth=%d)", len(out), len(ptt.runtime.playbackBuffer))
		default:
			ptt.Log.Warn().Msg("⚠️ Playback buffer full! Dropping packet.")
		}
	}
}

func (ptt *PTTConfig) monitorPTT(dev *evdev.InputDevice, bcastStream *portaudio.Stream) {
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
				ptt.endTransmission(bcastStream)
			} else {
				ptt.Log.Debug().Msgf("PTT toggle: starting transmission")
				ptt.beginTransmission(bcastStream)
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

func (ptt *PTTConfig) beginTransmission(bcastStream *portaudio.Stream) {
	if bcastStream == nil {
		ptt.Log.Warn().Msg("PTT audio input stream is unavailable; ignoring transmit request")
		return
	}

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

	if err := bcastStream.Start(); err != nil {
		ptt.Log.Error().Err(err).Msg("Failed to start mic stream")
		ptt.runtime.recordMutex.Lock()
		ptt.runtime.broadcasting = false
		ptt.runtime.recordMutex.Unlock()
		return
	}

	ptt.Log.Debug().Msg("Mic stream started")
}

func (ptt *PTTConfig) endTransmission(bcastStream *portaudio.Stream) {
	if bcastStream == nil {
		ptt.Log.Warn().Msg("PTT audio input stream is unavailable; ignoring transmit stop request")
		return
	}

	ptt.runtime.recordMutex.Lock()

	if !ptt.runtime.broadcasting {
		ptt.Log.Debug().Msgf("PTT up ignored; mic already idle")
		ptt.runtime.recordMutex.Unlock()
		return
	}

	ptt.runtime.recordMutex.Unlock()

	ptt.Log.Debug().Msg("End transmission: stopping mic stream and playing stop tone")
	if err := bcastStream.Stop(); err != nil {
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
