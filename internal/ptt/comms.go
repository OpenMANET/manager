package ptt

import (
	"net"
	"strconv"
	"time"

	"github.com/gordonklaus/portaudio"
	evdev "github.com/gvalkov/golang-evdev"
)

// receiveLoop continuously receives Opus-encoded audio from the UDP multicast stream,
// decodes it, and queues it for playback through the AIOC USB audio interface.
// This allows the operator to hear other stations transmitting on the mesh network.
func (ptt *PTTConfig) receiveLoop(udpConn *net.UDPConn) {
	buf := make([]byte, 1500)
	for {
		n, src, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			ptt.Log.Error().Err(err).Msg("Recv error")
			continue
		}

		ptt.Log.Debug().Msgf("Received %d bytes from %s", n, src.IP.String())
		if !loopbackAudio && (src.IP.IsLoopback() || src.IP.String() == localIP) {
			continue
		}

		frame := make([]byte, n)
		copy(frame, buf[:n])

		pcm := make([]int16, frameSize)
		n, err = decoder.Decode(frame, pcm)
		if err != nil {
			continue
		}
		out := make([]float32, n)
		for i := 0; i < n; i++ {
			out[i] = float32(pcm[i]) / 32768
		}

		select {
		case playbackBuffer <- out:
			ptt.Log.Debug().Msgf("Queued playback buffer with %d samples (depth=%d)", len(out), len(playbackBuffer))
		default:
			ptt.Log.Warn().Msg("⚠️ Playback buffer full! Dropping packet.")
		}
	}
}

// monitorPTT monitors the AIOC HID device for PTT button events.
// The AIOC firmware sends CM108-compatible HID events (Volume Up/Down buttons)
// when the PTT button is pressed. This uses push-to-talk mode:
// transmission starts when button is pressed and stops when released.
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
		case 1: // Button pressed
			ptt.Log.Info().Msgf("PTT button pressed (code=%d) - starting transmission", ev.Code)
			ptt.beginTransmission(bcastStream)
		case 0: // Button released
			ptt.Log.Info().Msgf("PTT button released (code=%d) - stopping transmission", ev.Code)
			if isBroadcasting() {
				ptt.endTransmission(bcastStream)
			}
		}
	}
}

func isBroadcasting() bool {
	recordMutex.Lock()
	defer recordMutex.Unlock()
	return broadcasting
}

func drainPlaybackBuffer() {
	for {
		select {
		case <-playbackBuffer:
		default:
			return
		}
	}
}

func (ptt *PTTConfig) beginTransmission(bcastStream *portaudio.Stream) {
	recordMutex.Lock()
	if broadcasting {
		ptt.Log.Debug().Msgf("PTT down ignored; already broadcasting")
		recordMutex.Unlock()
		return
	}
	broadcasting = true
	recordMutex.Unlock()

	ptt.Log.Debug().Msgf("Begin transmission: playing start tone and starting mic stream")
	drainPlaybackBuffer()
	playbackBuffer <- beepBufferStart
	time.Sleep(200 * time.Millisecond)

	if err := bcastStream.Start(); err != nil {
		ptt.Log.Error().Err(err).Msg("Failed to start mic stream")
		recordMutex.Lock()
		broadcasting = false
		recordMutex.Unlock()
		return
	}

	ptt.Log.Debug().Msg("Mic stream started")
}

func (ptt *PTTConfig) endTransmission(bcastStream *portaudio.Stream) {
	recordMutex.Lock()

	if !broadcasting {
		ptt.Log.Debug().Msgf("PTT up ignored; mic already idle")
		recordMutex.Unlock()
		return
	}

	recordMutex.Unlock()

	ptt.Log.Debug().Msg("End transmission: stopping mic stream and playing stop tone")
	if err := bcastStream.Stop(); err != nil {
		ptt.Log.Error().Err(err).Msg("stop mic")
	} else {
		ptt.Log.Debug().Msg("Mic stream stopped")
	}

	drainPlaybackBuffer()
	playbackBuffer <- beepBufferStop

	recordMutex.Lock()
	broadcasting = false
	recordMutex.Unlock()
}
