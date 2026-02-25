package ptt

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
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
	ptt := &PTTConfig{Log: zerolog.Nop(), AudioDeviceHint: "BT-Headset"}
	ptt.applyDefaults()

	if ptt.InputDevice != "BT-Headset" {
		t.Errorf("InputDevice: got %q, want BT-Headset", ptt.InputDevice)
	}
	if ptt.OutputDevice != "BT-Headset" {
		t.Errorf("OutputDevice: got %q, want BT-Headset", ptt.OutputDevice)
	}
}

func TestApplyDefaults_AudioHint_DoesNotOverrideExplicit(t *testing.T) {
	ptt := &PTTConfig{
		Log:             zerolog.Nop(),
		AudioDeviceHint: "BT-Headset",
		InputDevice:     "explicit-mic",
	}
	ptt.applyDefaults()

	if ptt.InputDevice != "explicit-mic" {
		t.Errorf("explicit InputDevice should not be overridden; got %q", ptt.InputDevice)
	}
	if ptt.OutputDevice != "BT-Headset" {
		t.Errorf("OutputDevice from hint: got %q, want BT-Headset", ptt.OutputDevice)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

type nopReadCloser struct {
	*bytes.Buffer
}

func (n *nopReadCloser) Close() error { return nil }

func newNopReadCloser(b *bytes.Buffer) *nopReadCloser {
	return &nopReadCloser{b}
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
	ptt := &PTTConfig{Log: zerolog.Nop(), ControlSource: "bluealsa_xevent"}
	ptt.applyDefaults()

	if ptt.ControlSource != "bluealsa_xevent" {
		t.Errorf("ControlSource: got %q, want %q", ptt.ControlSource, "bluealsa_xevent")
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

	checks := []struct {
		name      string
		got, want interface{}
	}{
		{"Enable", got.Enable, src.Enable},
		{"Iface", got.Iface, src.Iface},
		{"McastAddr", got.McastAddr, src.McastAddr},
		{"McastPort", got.McastPort, src.McastPort},
		{"Protocol", got.Protocol, src.Protocol},
		{"RtpID", got.RtpID, src.RtpID},
		{"InputDevice", got.InputDevice, src.InputDevice},
		{"OutputDevice", got.OutputDevice, src.OutputDevice},
		{"AudioDeviceHint", got.AudioDeviceHint, src.AudioDeviceHint},
		{"PlaybackDepth", got.PlaybackDepth, src.PlaybackDepth},
		{"PTTKey", got.PTTKey, src.PTTKey},
		{"PTTDeviceGlob", got.PTTDeviceGlob, src.PTTDeviceGlob},
		{"PTTDeviceName", got.PTTDeviceName, src.PTTDeviceName},
		{"ControlSource", got.ControlSource, src.ControlSource},
		{"Debug", got.Debug, src.Debug},
		{"Loopback", got.Loopback, src.Loopback},
		{"Trace", got.Trace, src.Trace},
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
