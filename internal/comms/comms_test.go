package comms

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func makeRTPBytes(t *testing.T, _ uint16) []byte {
	t.Helper()

	w := &mockWriter{}

	sess, err := newPionRTPSession(0x1234, w, &mockWriter{}, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.close() //nolint:errcheck

	if err := sess.send([]byte{0xAA, 0xBB}); err != nil {
		t.Fatal(err)
	}

	if len(w.Packets) == 0 {
		t.Fatal("no packets written")
	}

	return w.Packets[0]
}

func TestReceiveLoop_ExitsOnContextCancel(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	reader := newMockReader()
	rt := &CommsRuntime{
		playbackBuffer: make(chan []float32, 8),
		decoder:        &mockDecoder{},
		receiver:       newSwappableReceiver(reader),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, rt)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("receiveLoop did not exit after context cancel")
	}
}

func TestReceiveLoop_IngestsPackets(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}

	var pkts []mockPacket

	for i := 0; i < jitterPrebufferPackets+2; i++ {
		raw := makeRTPBytes(t, uint16(i))
		pkts = append(pkts, mockPacket{data: raw, src: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}})
	}

	reader := newMockReader(pkts...)
	rt := &CommsRuntime{
		playbackBuffer: make(chan []float32, 32),
		decoder:        &mockDecoder{returnN: int(rtpFrameSamples)},
		receiver:       newSwappableReceiver(reader),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, rt)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reader.remaining() == 0 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	rt.receiver.Close() // unblocks ReadFromUDP in mockReader

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit")
	}
}

func TestReceiveLoop_DiscardsDuringBroadcast(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}
	raw := makeRTPBytes(t, 0)
	reader := newMockReader(mockPacket{data: raw, src: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}})
	rt := &CommsRuntime{
		playbackBuffer: make(chan []float32, 8),
		decoder:        &mockDecoder{},
		broadcasting:   true,
		receiver:       newSwappableReceiver(reader),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, rt)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	rt.receiver.Close() // unblocks ReadFromUDP in mockReader

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit")
	}

	if len(rt.playbackBuffer) != 0 {
		t.Errorf("playback buffer should be empty during broadcast; got %d frames", len(rt.playbackBuffer))
	}
}

func TestDecodeAndQueue(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	buf := make(chan []float32, 4)
	dec := &mockDecoder{fillValue: 42, returnN: 4}
	rt := &CommsRuntime{playbackBuffer: buf, decoder: dec}
	cfg.decodeAndQueue(rt, []byte{1, 2, 3})

	if len(buf) != 1 {
		t.Fatalf("expected 1 frame; got %d", len(buf))
	}

	frame := <-buf
	// float32 arithmetic may produce minor rounding; verify the value is in the right ballpark.
	expected := float32(42) / 32768.0

	if len(frame) == 0 {
		t.Fatal("empty frame")
	}

	const eps = 0.0001

	for i, v := range frame {
		diff := v - expected
		if diff < -eps || diff > eps {
			t.Errorf("sample[%d]=%f want ~%f", i, v, expected)

			break
		}
	}
}

func TestDecodeAndQueuePLC(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	buf := make(chan []float32, 4)
	dec := &mockDecoder{fillValue: 10, returnN: 4}
	rt := &CommsRuntime{playbackBuffer: buf, decoder: dec}
	cfg.decodeAndQueuePLC(rt)

	if len(buf) != 1 {
		t.Fatalf("expected 1 PLC frame; got %d", len(buf))
	}
}

func TestUpdateMulticastEndpoint_InactiveError(t *testing.T) {
	err := UpdateMulticastEndpoint("239.1.2.3", 5004)
	if err == nil {
		t.Error("expected error when not running")
	}
}

func TestNewComms_Defaults(t *testing.T) {
	// NewComms is a copy constructor; defaults (McastPort, PlaybackDepth, etc.)
	// are applied lazily in Start(). Verify NewComms returns a non-nil value and
	// that the supplied log is preserved.
	log := zerolog.Nop()

	cfg := NewComms(CommsConfig{Log: log, McastPort: 5004})
	if cfg == nil {
		t.Fatal("NewComms returned nil")
	}

	if cfg.McastPort != 5004 {
		t.Errorf("McastPort: got %d, want 5004", cfg.McastPort)
	}
}

// ─── applyDefaults tests ──────────────────────────────────────────────────────

func TestApplyDefaults_AllEmptyGetsDefaults(t *testing.T) {
	// Ensure RtpID fallback can use the real hostname.
	os.Unsetenv("HOSTNAME") //nolint:errcheck

	cfg := &CommsConfig{}
	cfg.applyDefaults()

	if cfg.Iface != defaultIface {
		t.Errorf("Iface: got %q, want %q", cfg.Iface, defaultIface)
	}

	if cfg.McastAddr == "" {
		t.Error("McastAddr should be non-empty after applyDefaults")
	}

	if cfg.McastPort != defaultPort {
		t.Errorf("McastPort: got %d, want %d", cfg.McastPort, defaultPort)
	}

	if cfg.CommKey != defaultKey {
		t.Errorf("CommKey: got %q, want %q", cfg.CommKey, defaultKey)
	}

	if cfg.NanoPTTDevicePath != defaultCommDevice {
		t.Errorf("NanoPTTDevicePath: got %q, want %q", cfg.NanoPTTDevicePath, defaultCommDevice)
	}

	if cfg.NanoPTTDeviceName != defaultCommName {
		t.Errorf("NanoPTTDeviceName: got %q, want %q", cfg.NanoPTTDeviceName, defaultCommName)
	}
}

func TestApplyDefaults_ExistingValuesPreserved(t *testing.T) {
	cfg := &CommsConfig{
		Iface:             "eth0",
		McastAddr:         "239.1.2.3",
		McastPort:         9999,
		CommKey:           "42",
		NanoPTTDevicePath: "/dev/custom/*",
		NanoPTTDeviceName: "MyDevice",
		RtpID:             "explicit-id",
	}
	cfg.applyDefaults()

	if cfg.Iface != "eth0" {
		t.Errorf("Iface overwritten; got %q", cfg.Iface)
	}

	if cfg.McastAddr != "239.1.2.3" {
		t.Errorf("McastAddr overwritten; got %q", cfg.McastAddr)
	}

	if cfg.McastPort != 9999 {
		t.Errorf("McastPort overwritten; got %d", cfg.McastPort)
	}

	if cfg.CommKey != "42" {
		t.Errorf("CommKey overwritten; got %q", cfg.CommKey)
	}

	if cfg.NanoPTTDevicePath != "/dev/custom/*" {
		t.Errorf("NanoPTTDevicePath overwritten; got %q", cfg.NanoPTTDevicePath)
	}

	if cfg.NanoPTTDeviceName != "MyDevice" {
		t.Errorf("NanoPTTDeviceName overwritten; got %q", cfg.NanoPTTDeviceName)
	}

	if cfg.RtpID != "explicit-id" {
		t.Errorf("RtpID overwritten; got %q", cfg.RtpID)
	}
}

func TestApplyDefaults_RtpIDFallsBackToHostname(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		t.Skip("cannot determine hostname; skipping")
	}

	cfg := &CommsConfig{}
	cfg.applyDefaults()

	if cfg.RtpID != hostname {
		t.Errorf("RtpID: got %q, want %q (hostname)", cfg.RtpID, hostname)
	}
}

func TestApplyDefaults_BluetoothAudioDeviceHintPropagates(t *testing.T) {
	cfg := &CommsConfig{BluetoothAudioDeviceHint: "usb-audio"}
	cfg.applyDefaults()

	if cfg.BluetoothInputDevice != "usb-audio" {
		t.Errorf("BluetoothInputDevice: got %q, want %q", cfg.BluetoothInputDevice, "usb-audio")
	}

	if cfg.BluetoothOutputDevice != "usb-audio" {
		t.Errorf("BluetoothOutputDevice: got %q, want %q", cfg.BluetoothOutputDevice, "usb-audio")
	}
}

func TestApplyDefaults_BluetoothAudioDeviceHintDoesNotOverrideExplicit(t *testing.T) {
	cfg := &CommsConfig{
		BluetoothAudioDeviceHint: "usb-audio",
		BluetoothInputDevice:     "hw:0",
		BluetoothOutputDevice:    "hw:1",
	}
	cfg.applyDefaults()

	if cfg.BluetoothInputDevice != "hw:0" {
		t.Errorf("BluetoothInputDevice overwritten; got %q", cfg.BluetoothInputDevice)
	}

	if cfg.BluetoothOutputDevice != "hw:1" {
		t.Errorf("BluetoothOutputDevice overwritten; got %q", cfg.BluetoothOutputDevice)
	}
}

// ─── replaceNetwork tests ─────────────────────────────────────────────────────

func TestReplaceNetwork_ClosesOldReceiverAndSender(t *testing.T) {
	oldSender := &mockClosingWriter{}
	oldReceiver := &trackingReader{}

	rt := &CommsRuntime{
		sender:   newSwappableSender(oldSender),
		receiver: newSwappableReceiver(oldReceiver),
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.replaceNetwork(rt, &mockWriter{}, newMockReader(), "10.0.0.1")

	if !oldSender.closeCalled {
		t.Error("old sender Close() should have been called")
	}

	if !oldReceiver.closed {
		t.Error("old receiver Close() should have been called")
	}
}

func TestReplaceNetwork_StoresNewLocalIP(t *testing.T) {
	rt := &CommsRuntime{
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.replaceNetwork(rt, &mockWriter{}, newMockReader(), "10.0.0.2")

	v, ok := rt.localIP.Load().(string)
	if !ok || v != "10.0.0.2" {
		t.Errorf("localIP: got %v, want 10.0.0.2", rt.localIP.Load())
	}
}

func TestReplaceNetwork_NewWriterReceivesSubsequentWrites(t *testing.T) {
	newSender := &mockWriter{}

	rt := &CommsRuntime{
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.replaceNetwork(rt, newSender, newMockReader(), "10.0.0.3")

	if _, err := rt.sender.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	if len(newSender.Packets) != 1 {
		t.Errorf("new sender: got %d packets, want 1", len(newSender.Packets))
	}
}

// ─── UpdateMulticastEndpoint validation tests ─────────────────────────────────

// setupActiveConfig injects a minimal fake activeConfig and restores nil on cleanup.
func setupActiveConfig(t *testing.T) {
	t.Helper()

	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.runtime = &CommsRuntime{
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}

	activeConfig.Store(cfg)
	t.Cleanup(func() { activeConfig.Store(nil) })
}

func TestUpdateMulticastEndpoint_InvalidIP(t *testing.T) {
	setupActiveConfig(t)

	if err := UpdateMulticastEndpoint("not-an-ip", 5004); err == nil {
		t.Error("expected error for invalid IP string")
	}
}

func TestUpdateMulticastEndpoint_IPv6Address(t *testing.T) {
	setupActiveConfig(t)

	if err := UpdateMulticastEndpoint("ff02::1", 5004); err == nil {
		t.Error("expected error for IPv6 multicast address (only IPv4 supported)")
	}
}

func TestUpdateMulticastEndpoint_NonMulticastIP(t *testing.T) {
	setupActiveConfig(t)

	if err := UpdateMulticastEndpoint("10.0.0.1", 5004); err == nil {
		t.Error("expected error for non-multicast IP")
	}
}

func TestUpdateMulticastEndpoint_PortZero(t *testing.T) {
	setupActiveConfig(t)

	if err := UpdateMulticastEndpoint("239.1.2.3", 0); err == nil {
		t.Error("expected error for port 0")
	}
}

func TestUpdateMulticastEndpoint_PortTooLarge(t *testing.T) {
	setupActiveConfig(t)

	if err := UpdateMulticastEndpoint("239.1.2.3", 65536); err == nil {
		t.Error("expected error for port > 65535")
	}
}
