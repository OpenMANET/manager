package ptt

import (
	"log"
	"net"
	"strconv"
	"time"

	"github.com/gordonklaus/portaudio"
	evdev "github.com/gvalkov/golang-evdev"
)

func receiveLoop(udpConn *net.UDPConn) {
	buf := make([]byte, 1500)
	for {
		n, src, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			log.Println("Recv error:", err)
			continue
		}

		debugf("Received %d bytes from %s", n, src.IP.String())
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
			debugf("Queued playback buffer with %d samples (depth=%d)", len(out), len(playbackBuffer))
		default:
			log.Println("⚠️ Playback buffer full! Dropping packet.")
		}
	}
}

func monitorPTT(dev *evdev.InputDevice, bcastStream *portaudio.Stream) {
	for {
		ev, err := dev.ReadOne()
		if err != nil {
			continue
		}
		if ev.Type != evdev.EV_KEY {
			continue
		}
		match := false
		if pttKey == "any" {
			match = true
		} else if kc, err := strconv.Atoi(pttKey); err == nil && ev.Code == uint16(kc) {
			match = true
		}
		if !match {
			continue
		}

		switch ev.Value {
		case 1:
			debugf("PTT down (code=%d)", ev.Code)
			if isBroadcasting() {
				debugf("PTT toggle: stopping transmission")
				endTransmission(bcastStream)
			} else {
				debugf("PTT toggle: starting transmission")
				beginTransmission(bcastStream)
			}
		case 0:
			debugf("PTT up (code=%d)", ev.Code)
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

func beginTransmission(bcastStream *portaudio.Stream) {
	recordMutex.Lock()
	if broadcasting {
		debugf("PTT down ignored; already broadcasting")
		recordMutex.Unlock()
		return
	}
	broadcasting = true
	recordMutex.Unlock()

	debugf("Begin transmission: playing start tone and starting mic stream")
	drainPlaybackBuffer()
	playbackBuffer <- beepBufferStart
	time.Sleep(200 * time.Millisecond)

	if err := bcastStream.Start(); err != nil {
		log.Printf("start mic: %v", err)
		recordMutex.Lock()
		broadcasting = false
		recordMutex.Unlock()
		return
	}

	debugf("Mic stream started")
}

func endTransmission(bcastStream *portaudio.Stream) {
	recordMutex.Lock()

	if !broadcasting {
		debugf("PTT up ignored; mic already idle")
		recordMutex.Unlock()
		return
	}

	recordMutex.Unlock()

	debugf("End transmission: stopping mic stream and playing stop tone")
	if err := bcastStream.Stop(); err != nil {
		log.Printf("stop mic: %v", err)
	} else {
		debugf("Mic stream stopped")
	}

	drainPlaybackBuffer()
	playbackBuffer <- beepBufferStop

	recordMutex.Lock()
	broadcasting = false
	recordMutex.Unlock()
}
