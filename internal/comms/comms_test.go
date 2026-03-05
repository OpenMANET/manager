package comms

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
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
	pc := &portChannel{
		cfg:      McastPortConfig{Send: true, Receive: true},
		receiver: newSwappableReceiver(newMockReader()),
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []float32, 8)
	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, pc, rt)
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
	pc := &portChannel{
		cfg:      McastPortConfig{Send: true, Receive: true},
		receiver: newSwappableReceiver(reader),
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []float32, 32)
	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{returnN: int(rtpFrameSamples)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, pc, rt)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reader.remaining() == 0 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	pc.receiver.Close() // unblocks ReadFromUDP in mockReader

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit")
	}
}

// TestPlayoutLoop_SuppressedDuringBroadcastOnSendPort verifies that the playout
// loop (spawned by receiveLoop) suppresses output while broadcasting on a
// send-capable port. Receive-only ports are not suppressed; that behaviour is
// covered by TestPlayoutLoop_ReceiveOnlyPortNotSuppressedDuringBroadcast.
func TestPlayoutLoop_SuppressedDuringBroadcastOnSendPort(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}
	raw := makeRTPBytes(t, 0)
	reader := newMockReader(mockPacket{data: raw, src: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}})
	pc := &portChannel{
		cfg:      McastPortConfig{Send: true, Receive: true},
		receiver: newSwappableReceiver(reader),
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []float32, 8)
	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{},
	}
	rt.broadcasting.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, pc, rt)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	pc.receiver.Close() // unblocks ReadFromUDP in mockReader

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit")
	}

	if len(pc.playbackBuffer) != 0 {
		t.Errorf("playback buffer should be empty during broadcast; got %d frames", len(pc.playbackBuffer))
	}
}

func TestDecodeAndQueue(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	buf := make(chan []float32, 4)
	pc := &portChannel{}
	pc.playbackBuffer = buf
	dec := &mockDecoder{fillValue: 42, returnN: 4}
	rt := &CommsRuntime{decoder: dec}
	cfg.decodeAndQueue(pc, rt, []byte{1, 2, 3})

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
	pc := &portChannel{}
	pc.playbackBuffer = buf
	dec := &mockDecoder{fillValue: 10, returnN: 4}
	rt := &CommsRuntime{decoder: dec}
	cfg.decodeAndQueuePLC(pc, rt)

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
	// NewComms is a copy constructor; defaults (McastPorts, PlaybackDepth, etc.)
	// are applied lazily in Start(). Verify NewComms returns a non-nil value and
	// that the supplied log is preserved.
	log := zerolog.Nop()

	cfg := NewComms(CommsConfig{Log: log, McastPorts: []McastPortConfig{{Port: 5004, Send: true, Receive: true}}})
	if cfg == nil {
		t.Fatal("NewComms returned nil")
	}

	if cfg.McastPorts[0].Port != 5004 {
		t.Errorf("McastPorts[0].Port: got %d, want 5004", cfg.McastPorts[0].Port)
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

	if cfg.McastPorts[0].Address == "" {
		t.Error("McastPorts[0].Address should be non-empty after applyDefaults")
	}

	if cfg.McastPorts[0].Port != config.DefaultTalkGroupPort {
		t.Errorf("McastPorts[0].Port: got %d, want %d", cfg.McastPorts[0].Port, config.DefaultTalkGroupPort)
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
		Iface: "eth0",
		McastPorts: []McastPortConfig{{
			Address: "239.1.2.3",
			Port:    9999,
			Send:    true,
			Receive: true,
		}},
		CommKey:           "42",
		NanoPTTDevicePath: "/dev/custom/*",
		NanoPTTDeviceName: "MyDevice",
		RtpID:             "explicit-id",
	}
	cfg.applyDefaults()

	if cfg.Iface != "eth0" {
		t.Errorf("Iface overwritten; got %q", cfg.Iface)
	}

	if cfg.McastPorts[0].Address != "239.1.2.3" {
		t.Errorf("McastPorts[0].Address overwritten; got %q", cfg.McastPorts[0].Address)
	}

	if cfg.McastPorts[0].Port != 9999 {
		t.Errorf("McastPorts[0].Port overwritten; got %d", cfg.McastPorts[0].Port)
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
	oldRTCP := &mockClosingWriter{}

	pc := &portChannel{
		sender:   newSwappableSender(oldSender),
		rtcpSend: newSwappableSender(oldRTCP),
		receiver: newSwappableReceiver(oldReceiver),
	}
	rt := &CommsRuntime{
		ports: []*portChannel{pc},
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.replaceNetwork(rt, 0, &mockWriter{}, &mockWriter{}, newMockReader(), "10.0.0.1")

	if !oldSender.closeCalled {
		t.Error("old sender Close() should have been called")
	}

	if !oldReceiver.closed {
		t.Error("old receiver Close() should have been called")
	}

	if !oldRTCP.closeCalled {
		t.Error("old RTCP sender Close() should have been called")
	}
}

func TestReplaceNetwork_StoresNewLocalIP(t *testing.T) {
	pc := &portChannel{
		sender:   newSwappableSender(&mockWriter{}),
		rtcpSend: newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}
	rt := &CommsRuntime{
		ports: []*portChannel{pc},
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.replaceNetwork(rt, 0, &mockWriter{}, &mockWriter{}, newMockReader(), "10.0.0.2")

	v, ok := rt.localIP.Load().(string)
	if !ok || v != "10.0.0.2" {
		t.Errorf("localIP: got %v, want 10.0.0.2", rt.localIP.Load())
	}
}

func TestReplaceNetwork_NewWriterReceivesSubsequentWrites(t *testing.T) {
	newSender := &mockWriter{}

	pc := &portChannel{
		sender:   newSwappableSender(&mockWriter{}),
		rtcpSend: newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}
	rt := &CommsRuntime{
		ports: []*portChannel{pc},
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.replaceNetwork(rt, 0, newSender, &mockWriter{}, newMockReader(), "10.0.0.3")

	if _, err := pc.sender.Write([]byte{1, 2, 3}); err != nil {
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

	pc := &portChannel{
		cfg:      McastPortConfig{Address: "239.0.0.1", Port: 5004, Send: true, Receive: true},
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	cfg := &CommsConfig{
		Log:        zerolog.Nop(),
		McastPorts: []McastPortConfig{pc.cfg},
	}
	cfg.runtime = &CommsRuntime{
		ports: []*portChannel{pc},
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

// ─── GetMulticastAddr tests ───────────────────────────────────────────────────

func TestGetActiveMulticastAddr_NotStarted(t *testing.T) {
	// Ensure no active config is set.
	activeConfig.Store(nil)

	if got := GetActiveMulticastAddr(); got != "" {
		t.Errorf("expected empty string when comms not started, got %q", got)
	}
}

func TestGetActiveMulticastAddr_ReturnsConfiguredAddr(t *testing.T) {
	const want = "239.1.2.3"

	cfg := &CommsConfig{
		Log:        zerolog.Nop(),
		McastPorts: []McastPortConfig{{Address: want, Port: 5004, Send: true, Receive: true}},
	}
	pc := &portChannel{
		cfg:      cfg.McastPorts[0],
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}
	cfg.runtime = &CommsRuntime{
		ports: []*portChannel{pc},
	}

	activeConfig.Store(cfg)
	t.Cleanup(func() { activeConfig.Store(nil) })

	if got := GetActiveMulticastAddr(); got != want {
		t.Errorf("GetActiveMulticastAddr() = %q, want %q", got, want)
	}
}

func TestGetActiveMulticastAddr_ReflectsUpdate(t *testing.T) {
	const (
		initial = "239.0.0.1"
		updated = "239.9.9.9"
	)

	cfg := &CommsConfig{
		Log:        zerolog.Nop(),
		McastPorts: []McastPortConfig{{Address: initial, Port: 5004, Send: true, Receive: true}},
	}
	pc := &portChannel{
		cfg:      cfg.McastPorts[0],
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}
	cfg.runtime = &CommsRuntime{
		ports: []*portChannel{pc},
	}

	activeConfig.Store(cfg)
	t.Cleanup(func() { activeConfig.Store(nil) })

	if got := GetActiveMulticastAddr(); got != initial {
		t.Errorf("before update: GetActiveMulticastAddr() = %q, want %q", got, initial)
	}

	cfg.McastPorts[0] = McastPortConfig{Address: updated, Port: 5004, Send: true, Receive: true}

	if got := GetActiveMulticastAddr(); got != updated {
		t.Errorf("after update: GetActiveMulticastAddr() = %q, want %q", got, updated)
	}
}

// ─── listenRTPReceiver (SO_REUSEPORT) tests ───────────────────────────────────

// ─── GetActiveMulticastPort tests ───────────────────────────────────────────

func TestGetActiveMulticastPort_NotStarted(t *testing.T) {
	activeConfig.Store(nil)

	if got := GetActiveMulticastPort(); got != 0 {
		t.Errorf("expected 0 when comms not started, got %d", got)
	}
}

func TestGetActiveMulticastPort_ReturnsConfiguredPort(t *testing.T) {
	const want = 5004

	cfg := &CommsConfig{
		Log:        zerolog.Nop(),
		McastPorts: []McastPortConfig{{Address: "239.1.2.3", Port: want, Send: true, Receive: true}},
	}
	pc := &portChannel{
		cfg:      cfg.McastPorts[0],
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}
	cfg.runtime = &CommsRuntime{
		ports: []*portChannel{pc},
	}

	activeConfig.Store(cfg)
	t.Cleanup(func() { activeConfig.Store(nil) })

	if got := GetActiveMulticastPort(); got != want {
		t.Errorf("GetActiveMulticastPort() = %d, want %d", got, want)
	}
}

// ─── UpdateMulticastEndpoint multi-port tests ─────────────────────────────────

func TestUpdateMulticastEndpoint_MultiplePortsError(t *testing.T) {
	pc0 := &portChannel{
		cfg:      McastPortConfig{Address: "239.0.0.1", Port: 5004, Send: true, Receive: true},
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}
	pc1 := &portChannel{
		cfg:      McastPortConfig{Address: "239.0.0.2", Port: 5006, Send: true, Receive: true},
		sender:   newSwappableSender(&mockWriter{}),
		receiver: newSwappableReceiver(newMockReader()),
	}

	cfg := &CommsConfig{
		Log:        zerolog.Nop(),
		McastPorts: []McastPortConfig{pc0.cfg, pc1.cfg},
	}
	cfg.runtime = &CommsRuntime{
		ports: []*portChannel{pc0, pc1},
	}

	activeConfig.Store(cfg)
	t.Cleanup(func() { activeConfig.Store(nil) })

	if err := UpdateMulticastEndpoint("239.1.2.3", 5004); err == nil {
		t.Error("expected error when more than one McastPort is configured")
	}
}

// TestListenRTPReceiver_ReusePort verifies that two sockets can be bound to the
// same port simultaneously — the invariant that makes UpdateMulticastEndpoint
// safe when the port does not change.
func TestListenRTPReceiver_ReusePort(t *testing.T) {
	first, err := listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	defer first.Close() //nolint:errcheck

	port := first.LocalAddr().(*net.UDPAddr).Port

	second, err := listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		t.Fatalf("second listen on same port %d: %v (SO_REUSEPORT not working)", port, err)
	}

	defer second.Close() //nolint:errcheck
}

// TestListenRTPReceiver_ReturnType verifies that listenRTPReceiver returns a
// *net.UDPConn (required by joinMulticastGroup).
func TestListenRTPReceiver_ReturnType(t *testing.T) {
	conn, err := listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck

	if conn == nil {
		t.Fatal("expected non-nil *net.UDPConn")
	}
}
