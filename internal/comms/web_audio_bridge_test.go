package comms

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestWebAudioBridge_InjectTxFrame_SendsToAllPorts(t *testing.T) {
	rtp0 := &mockRTPSender{}
	rtp1 := &mockRTPSender{}

	pc0 := &portChannel{cfg: McastPortConfig{Send: true}, rtpSess: rtp0}
	pc0.sendEnabled.Store(true)

	pc1 := &portChannel{cfg: McastPortConfig{Send: true}, rtpSess: rtp1}
	pc1.sendEnabled.Store(true)

	cfg := newSilentComms()
	rt := &CommsRuntime{ports: []*portChannel{pc0, pc1}}
	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())

	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	bridge.InjectTxFrame(payload)

	for i, rtp := range []*mockRTPSender{rtp0, rtp1} {
		rtp.mu.Lock()
		count := len(rtp.Payloads)
		rtp.mu.Unlock()

		if count != 1 {
			t.Errorf("port %d: expected 1 payload, got %d", i, count)
		}
	}
}

func TestWebAudioBridge_InjectTxFrame_SkipsDisabledPorts(t *testing.T) {
	rtp0 := &mockRTPSender{}
	rtp1 := &mockRTPSender{}

	pc0 := &portChannel{cfg: McastPortConfig{Send: true}, rtpSess: rtp0}
	pc0.sendEnabled.Store(true)

	pc1 := &portChannel{cfg: McastPortConfig{Send: true}, rtpSess: rtp1}
	pc1.sendEnabled.Store(false) // disabled

	cfg := newSilentComms()
	rt := &CommsRuntime{ports: []*portChannel{pc0, pc1}}
	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())

	bridge.InjectTxFrame([]byte{0x01})

	rtp0.mu.Lock()
	count0 := len(rtp0.Payloads)
	rtp0.mu.Unlock()

	rtp1.mu.Lock()
	count1 := len(rtp1.Payloads)
	rtp1.mu.Unlock()

	if count0 != 1 {
		t.Errorf("enabled port: expected 1 payload, got %d", count0)
	}

	if count1 != 0 {
		t.Errorf("disabled port: expected 0 payloads, got %d", count1)
	}
}

func TestWebAudioBridge_PushRxFrame_Delivered(t *testing.T) {
	cfg := newSilentComms()
	rt := &CommsRuntime{}
	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())

	data := []byte{0xCA, 0xFE}
	bridge.PushRxFrame(data)

	select {
	case got := <-bridge.RxFrames():
		if len(got) != 2 || got[0] != 0xCA || got[1] != 0xFE {
			t.Errorf("unexpected frame data: %v", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timed out waiting for RX frame")
	}
}

func TestWebAudioBridge_PushRxFrame_DropsOnFull(t *testing.T) {
	cfg := newSilentComms()
	rt := &CommsRuntime{}
	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())

	// Fill the channel.
	for i := 0; i < 50; i++ {
		bridge.PushRxFrame([]byte{byte(i)})
	}

	// This push must not block.
	done := make(chan struct{})

	go func() {
		bridge.PushRxFrame([]byte{0xFF})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("PushRxFrame blocked on full channel")
	}
}

func TestWebAudioBridge_RxFrames_ReturnsReadOnlyChannel(t *testing.T) {
	cfg := newSilentComms()
	rt := &CommsRuntime{}
	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())

	ch := bridge.RxFrames()
	if ch == nil {
		t.Error("RxFrames() returned nil")
	}

	// Verify it is a receive-only channel (compile-time check via type).
	var _ <-chan []byte = ch
}
