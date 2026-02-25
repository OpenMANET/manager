package ptt

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

const (
	testBTHeadset      = "BT-Headset"
	testBlueAlsaXEvent = "bluealsa_xevent"
)

// ─── applyDefaults ────────────────────────────────────────────────────────────

func TestApplyDefaults_FillsEmpty(t *testing.T) {
	ptt := &PTTConfig{Log: zerolog.Nop()}
	ptt.applyDefaults()

	if ptt.Iface != defaultIface {
		t.Errorf("Iface: got %q, want %q", ptt.Iface, defaultIface)
	}

	if ptt.McastAddr != defaultG {
		t.Errorf("McastAddr: got %q, want %q", ptt.McastAddr, defaultG)
	}

	if ptt.McastPort != defaultPort {
		t.Errorf("McastPort: got %d, want %d", ptt.McastPort, defaultPort)
	}

	if ptt.PTTKey != defaultKey {
		t.Errorf("PTTKey: got %q, want %q", ptt.PTTKey, defaultKey)
	}

	if ptt.Protocol != protocolUDP {
		t.Errorf("Protocol: got %q, want %q", ptt.Protocol, protocolUDP)
	}
}

func TestApplyDefaults_PreservesExplicit(t *testing.T) {
	ptt := &PTTConfig{
		Log:       zerolog.Nop(),
		Iface:     "wlan0",
		McastAddr: "239.1.2.3",
		McastPort: 9999,
		Protocol:  "rtp",
	}
	ptt.applyDefaults()

	if ptt.Iface != "wlan0" {
		t.Errorf("Iface should be preserved; got %q", ptt.Iface)
	}

	if ptt.McastAddr != "239.1.2.3" {
		t.Errorf("McastAddr should be preserved; got %q", ptt.McastAddr)
	}

	if ptt.McastPort != 9999 {
		t.Errorf("McastPort should be preserved; got %d", ptt.McastPort)
	}

	if ptt.Protocol != protocolRTP {
		t.Errorf("Protocol should be normalized to rtp; got %q", ptt.Protocol)
	}
}

func TestApplyDefaults_AudioHint_FillsBothDevices(t *testing.T) {
	ptt := &PTTConfig{Log: zerolog.Nop(), AudioDeviceHint: testBTHeadset}
	ptt.applyDefaults()

	if ptt.InputDevice != testBTHeadset {
		t.Errorf("InputDevice: got %q, want BT-Headset", ptt.InputDevice)
	}

	if ptt.OutputDevice != testBTHeadset {
		t.Errorf("OutputDevice: got %q, want BT-Headset", ptt.OutputDevice)
	}
}

func TestApplyDefaults_AudioHint_DoesNotOverrideExplicit(t *testing.T) {
	ptt := &PTTConfig{
		Log:             zerolog.Nop(),
		AudioDeviceHint: testBTHeadset,
		InputDevice:     "explicit-mic",
	}
	ptt.applyDefaults()

	if ptt.InputDevice != "explicit-mic" {
		t.Errorf("explicit InputDevice should not be overridden; got %q", ptt.InputDevice)
	}

	if ptt.OutputDevice != testBTHeadset {
		t.Errorf("OutputDevice from hint: got %q, want BT-Headset", ptt.OutputDevice)
	}
}

// ─── applyDefaults: additional coverage ──────────────────────────────────────

func TestApplyDefaults_PTTDeviceGlobDefault(t *testing.T) {
	ptt := &PTTConfig{Log: zerolog.Nop()}
	ptt.applyDefaults()

	if ptt.PTTDeviceGlob != defaultPTTDevice {
		t.Errorf("PTTDeviceGlob: got %q, want %q", ptt.PTTDeviceGlob, defaultPTTDevice)
	}

	if ptt.PTTDeviceName != defaultPTTDeviceName {
		t.Errorf("PTTDeviceName: got %q, want %q", ptt.PTTDeviceName, defaultPTTDeviceName)
	}
}

func TestApplyDefaults_ControlSourceDefault(t *testing.T) {
	ptt := &PTTConfig{Log: zerolog.Nop()}
	ptt.applyDefaults()

	if ptt.ControlSource != "evdev" {
		t.Errorf("ControlSource: got %q, want %q", ptt.ControlSource, "evdev")
	}
}

func TestApplyDefaults_ControlSourcePreserved(t *testing.T) {
	ptt := &PTTConfig{Log: zerolog.Nop(), ControlSource: testBlueAlsaXEvent}
	ptt.applyDefaults()

	if ptt.ControlSource != testBlueAlsaXEvent {
		t.Errorf("ControlSource: got %q, want %q", ptt.ControlSource, testBlueAlsaXEvent)
	}
}

// ─── NewPTT ───────────────────────────────────────────────────────────────────

func TestNewPTT_CopiesAllFields(t *testing.T) {
	interrupt := make(chan os.Signal, 1)
	src := PTTConfig{
		Log:             zerolog.Nop(),
		Interrupt:       interrupt,
		Enable:          true,
		Iface:           "wlan0",
		McastAddr:       "239.1.2.3",
		McastPort:       9999,
		Protocol:        "rtp",
		RtpID:           "node-1",
		InputDevice:     "mic",
		OutputDevice:    "spk",
		AudioDeviceHint: "hint",
		PlaybackDepth:   4,
		PTTKey:          "30",
		PTTDeviceGlob:   "/dev/input/event*",
		PTTDeviceName:   "MyDevice",
		ControlSource:   "evdev",
		Debug:           true,
		Loopback:        true,
		Trace:           true,
	}

	got := NewPTT(src)

	checks := []struct { //nolint:govet
		name string
		got  interface{}
		want interface{}
	}{
		{name: "Enable", got: got.Enable, want: src.Enable},
		{name: "Iface", got: got.Iface, want: src.Iface},
		{name: "McastAddr", got: got.McastAddr, want: src.McastAddr},
		{name: "McastPort", got: got.McastPort, want: src.McastPort},
		{name: "Protocol", got: got.Protocol, want: src.Protocol},
		{name: "RtpID", got: got.RtpID, want: src.RtpID},
		{name: "InputDevice", got: got.InputDevice, want: src.InputDevice},
		{name: "OutputDevice", got: got.OutputDevice, want: src.OutputDevice},
		{name: "AudioDeviceHint", got: got.AudioDeviceHint, want: src.AudioDeviceHint},
		{name: "PlaybackDepth", got: got.PlaybackDepth, want: src.PlaybackDepth},
		{name: "PTTKey", got: got.PTTKey, want: src.PTTKey},
		{name: "PTTDeviceGlob", got: got.PTTDeviceGlob, want: src.PTTDeviceGlob},
		{name: "PTTDeviceName", got: got.PTTDeviceName, want: src.PTTDeviceName},
		{name: "ControlSource", got: got.ControlSource, want: src.ControlSource},
		{name: "Debug", got: got.Debug, want: src.Debug},
		{name: "Loopback", got: got.Loopback, want: src.Loopback},
		{name: "Trace", got: got.Trace, want: src.Trace},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("NewPTT %s: got %v, want %v", c.name, c.got, c.want)
		}
	}

	if got.Interrupt != interrupt {
		t.Error("NewPTT Interrupt: channel not preserved")
	}

	if got.runtime != nil {
		t.Error("NewPTT should not copy runtime field")
	}
}

// ─── Run event loop ───────────────────────────────────────────────────────────

// newRunTestRuntime returns a PTTRuntime suitable for Run tests: has a
// broadcast stream, empty blocking receiver, and populated beep buffers.
func newRunTestRuntime(stream AudioStream, reader *mockReader) *PTTRuntime {
	rt := newTestRuntime(stream)
	rt.receiver = newSwappableReceiver(reader)

	return rt
}

func TestRun_ContextCancellation_Exits(t *testing.T) {
	ms := &mockStream{}
	reader := newMockReader()
	rt := newRunTestRuntime(ms, reader)
	ptt := newSilentPTT()

	src := &mockEventSource{ch: make(chan PTTEvent)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so Run exits on the first select

	defer reader.Close()

	done := make(chan struct{})

	go func() {
		ptt.Run(ctx, rt, src)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("Run did not exit after context cancellation")
	}
}

func TestRun_PTTDown_BeginsBroadcast(t *testing.T) {
	ms := &mockStream{}
	reader := newMockReader()
	rt := newRunTestRuntime(ms, reader)
	ptt := newSilentPTT()

	evCh := make(chan PTTEvent, 1)
	evCh <- PTTDown

	src := &mockEventSource{ch: evCh}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	defer reader.Close()

	go ptt.Run(ctx, rt, src)

	time.Sleep(450 * time.Millisecond)

	if !ptt.isBroadcasting(rt) {
		t.Error("expected broadcasting=true after PTTDown")
	}

	if ms.startCalls == 0 {
		t.Error("expected broadcastStream.Start() to be called on PTTDown")
	}
}

func TestRun_PTTUp_EndsBroadcast(t *testing.T) {
	ms := &mockStream{}
	reader := newMockReader()
	rt := newRunTestRuntime(ms, reader)
	ptt := newSilentPTT()

	evCh := make(chan PTTEvent, 2)
	evCh <- PTTDown

	evCh <- PTTUp

	src := &mockEventSource{ch: evCh}

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	defer reader.Close()

	go ptt.Run(ctx, rt, src)
	// beginTransmission sleeps 200ms; let both events process.
	time.Sleep(600 * time.Millisecond)

	if ptt.isBroadcasting(rt) {
		t.Error("expected broadcasting=false after PTTUp")
	}

	if ms.stopCalls == 0 {
		t.Error("expected broadcastStream.Stop() to be called on PTTUp")
	}
}

func TestRun_PTTToggle_StartsWhenIdle(t *testing.T) {
	ms := &mockStream{}
	reader := newMockReader()
	rt := newRunTestRuntime(ms, reader)
	ptt := newSilentPTT()

	evCh := make(chan PTTEvent, 1)
	evCh <- PTTToggle

	src := &mockEventSource{ch: evCh}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	defer reader.Close()

	go ptt.Run(ctx, rt, src)

	time.Sleep(450 * time.Millisecond)

	if !ptt.isBroadcasting(rt) {
		t.Error("expected broadcasting=true after PTTToggle when idle")
	}
}

func TestRun_EventChannelClosed_Exits(t *testing.T) {
	ms := &mockStream{}
	reader := newMockReader()
	rt := newRunTestRuntime(ms, reader)
	ptt := newSilentPTT()

	evCh := make(chan PTTEvent)
	close(evCh) // immediately closed
	src := &mockEventSource{ch: evCh}

	ctx := context.Background()

	defer reader.Close()

	done := make(chan struct{})

	go func() {
		ptt.Run(ctx, rt, src)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("Run did not exit after event channel was closed")
	}
}
