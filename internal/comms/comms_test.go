package comms

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"golang.org/x/net/ipv4"
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

// TestPlayoutOneFrame_SuppressedDuringBroadcastOnSendPort verifies that
// playoutOneFrame emits silence on a send-capable port while the local node
// is broadcasting (half-duplex echo prevention). Receive-only ports are not
// suppressed; that behavior is covered by
// TestPlayoutOneFrame_ReceiveOnlyPortNotSuppressedDuringBroadcast.
func TestPlayoutOneFrame_SuppressedDuringBroadcastOnSendPort(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	pc := &portChannel{
		cfg: McastPortConfig{Send: true, Receive: true},
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)

	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{fillValue: 42, returnN: frameSize},
	}
	rt.broadcasting.Store(true)

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA, 0xBB})

	out := make([]float32, frameSize)
	cfg.playoutOneFrame(pc, rt, jb, out)

	for i, v := range out {
		if v != 0 {
			t.Errorf("playoutOneFrame should emit silence during broadcast; sample[%d]=%f", i, v)

			break
		}
	}
}

func TestPlayoutOneFrame_DecodesPayloadIntoOut(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	pc := &portChannel{
		cfg: McastPortConfig{Send: true, Receive: true},
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)

	dec := &mockDecoder{fillValue: 42, returnN: frameSize}
	rt := &CommsRuntime{decoder: dec}

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{1, 2, 3})

	out := make([]float32, frameSize)
	cfg.playoutOneFrame(pc, rt, jb, out)

	// float32 arithmetic may produce minor rounding; verify the value is in the right ballpark.
	expected := float32(42) / 32768.0

	const eps = 0.0001

	for i, v := range out {
		diff := v - expected
		if diff < -eps || diff > eps {
			t.Errorf("sample[%d]=%f want ~%f", i, v, expected)

			break
		}
	}
}

func TestPlayoutOneFrame_PLCFillsOut(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	pc := &portChannel{
		cfg: McastPortConfig{Send: true, Receive: true},
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)

	dec := &mockDecoder{fillValue: 10, returnN: frameSize}
	rt := &CommsRuntime{decoder: dec}

	// Push and pop to set started=true and a recent lastPush; the next
	// playoutOneFrame call will hit the conceal branch and call the decoder
	// with a nil payload (PLC).
	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0})
	jb.popReady()

	out := make([]float32, frameSize)
	cfg.playoutOneFrame(pc, rt, jb, out)

	expected := float32(10) / 32768.0

	const eps = 0.0001

	if v := out[0]; v < expected-eps || v > expected+eps {
		t.Errorf("PLC sample[0]=%f want ~%f", v, expected)
	}
}

func TestNewComms_Defaults(t *testing.T) {
	// NewComms is a copy constructor; defaults (McastPorts, etc.) are applied
	// lazily in Start(). Verify NewComms returns a non-nil value and that the
	// supplied log is preserved.
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
	cfg.replaceNetwork(rt, &mockWriter{}, &mockWriter{}, newMockReader(), "10.0.0.1")

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
	cfg.replaceNetwork(rt, &mockWriter{}, &mockWriter{}, newMockReader(), "10.0.0.2")

	v, ok := rt.localIP.Load(), rt.localIP.Load() != nil
	if !ok || *v != "10.0.0.2" {
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
	cfg.replaceNetwork(rt, newSender, &mockWriter{}, newMockReader(), "10.0.0.3")

	if _, err := pc.sender.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	if len(newSender.Packets) != 1 {
		t.Errorf("new sender: got %d packets, want 1", len(newSender.Packets))
	}
}

// ─── UpdateMulticastEndpoint validation tests ─────────────────────────────────

// ─── GetMulticastAddr tests ───────────────────────────────────────────────────

func TestGetActiveMulticastAddr_NotStarted(t *testing.T) {
	// Ensure no active config is set.
	SetDefault(nil)

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

	SetDefault(cfg)
	t.Cleanup(func() { SetDefault(nil) })

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

	SetDefault(cfg)
	t.Cleanup(func() { SetDefault(nil) })

	if got := GetActiveMulticastAddr(); got != initial {
		t.Errorf("before update: GetActiveMulticastAddr() = %q, want %q", got, initial)
	}

	cfg.McastPorts[0] = McastPortConfig{Address: updated, Port: 5004, Send: true, Receive: true}

	if got := GetActiveMulticastAddr(); got != updated {
		t.Errorf("after update: GetActiveMulticastAddr() = %q, want %q", got, updated)
	}
}

// ─── setMulticastTTL tests ─────────────────────────────────────────────────────

func TestSetMulticastTTL(t *testing.T) {
	dst := &net.UDPAddr{IP: net.ParseIP("239.0.0.1"), Port: 0}
	src := &net.UDPAddr{IP: net.IPv4zero, Port: 0}

	conn, err := net.DialUDP("udp4", src, dst)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	if err := setMulticastTTL(conn, rtpMulticastTTL); err != nil {
		t.Fatalf("setMulticastTTL: %v", err)
	}

	got, err := ipv4.NewPacketConn(conn).MulticastTTL()
	if err != nil {
		t.Fatalf("MulticastTTL: %v", err)
	}

	if got != rtpMulticastTTL {
		t.Errorf("multicast TTL = %d, want %d", got, rtpMulticastTTL)
	}
}

// ─── listenRTPReceiver (SO_REUSEPORT) tests ───────────────────────────────────

// ─── GetActiveMulticastPort tests ───────────────────────────────────────────

func TestGetActiveMulticastPort_NotStarted(t *testing.T) {
	SetDefault(nil)

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

	SetDefault(cfg)
	t.Cleanup(func() { SetDefault(nil) })

	if got := GetActiveMulticastPort(); got != want {
		t.Errorf("GetActiveMulticastPort() = %d, want %d", got, want)
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

// TestGetReadBufferBytes_AfterSetReadBuffer verifies that the helper observes
// the value the kernel actually granted after a SetReadBuffer call. The
// returned value is at least as large as the previous default (65535) so the
// rxSocketBufBytes bump is a strict improvement, but it does not have to be
// exactly rxSocketBufBytes because Linux may clamp at net.core.rmem_max.
func TestGetReadBufferBytes_AfterSetReadBuffer(t *testing.T) {
	conn, err := listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck

	if err := conn.SetReadBuffer(rxSocketBufBytes); err != nil {
		t.Fatalf("SetReadBuffer(%d): %v", rxSocketBufBytes, err)
	}

	got, err := getReadBufferBytes(conn)
	if err != nil {
		t.Fatalf("getReadBufferBytes: %v", err)
	}

	// Linux returns SO_RCVBUF doubled (kernel bookkeeping overhead is folded
	// into the reported value). On a stock kernel with low rmem_max we may
	// be clamped well below rxSocketBufBytes; the only safe assertion is that
	// it is strictly larger than the previous hard-coded default (65535).
	const previousDefault = 65535
	if got <= previousDefault {
		t.Errorf("SO_RCVBUF after SetReadBuffer: got %d, want > %d (the previous default)", got, previousDefault)
	}
}

// ─── boolPtrVal tests ────────────────────────────────────────────────────────

func TestBoolPtrVal(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		p        *bool
		fallback bool
		want     bool
	}{
		{"nil pointer, fallback true", nil, true, true},
		{"nil pointer, fallback false", nil, false, false},
		{"non-nil true, fallback false", &trueVal, false, true},
		{"non-nil false, fallback true", &falseVal, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boolPtrVal(tt.p, tt.fallback)
			if got != tt.want {
				t.Errorf("boolPtrVal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ─── applyDefaults ROIP tests ────────────────────────────────────────────────

func TestApplyDefaults_ROIPDefaults(t *testing.T) {
	cfg := &CommsConfig{ControlSource: "roip"}
	cfg.applyDefaults()

	if cfg.ROIPCOSGPIOMask != roipDefaultCOSMask {
		t.Errorf("ROIPCOSGPIOMask: got %d, want %d", cfg.ROIPCOSGPIOMask, roipDefaultCOSMask)
	}

	if cfg.ROIPVOXThreshold != roipDefaultVOXThresh {
		t.Errorf("ROIPVOXThreshold: got %f, want %f", cfg.ROIPVOXThreshold, roipDefaultVOXThresh)
	}

	if cfg.ROIPVOXHoldTime != roipDefaultVOXHold {
		t.Errorf("ROIPVOXHoldTime: got %v, want %v", cfg.ROIPVOXHoldTime, roipDefaultVOXHold)
	}

	if cfg.ROIPMaxTXDuration != roipDefaultMaxTX {
		t.Errorf("ROIPMaxTXDuration: got %v, want %v", cfg.ROIPMaxTXDuration, roipDefaultMaxTX)
	}
}

func TestApplyDefaults_ROIPInputDeviceFallsBackToBluetooth(t *testing.T) {
	cfg := &CommsConfig{
		ControlSource:        "roip",
		BluetoothInputDevice: "hw:1",
	}
	cfg.applyDefaults()

	if cfg.ROIPInputDevice != "hw:1" {
		t.Errorf("ROIPInputDevice: got %q, want %q", cfg.ROIPInputDevice, "hw:1")
	}
}

func TestApplyDefaults_ROIPExplicitValuesPreserved(t *testing.T) {
	cfg := &CommsConfig{
		ControlSource:     "roip",
		ROIPCOSGPIOMask:   0x04,
		ROIPVOXThreshold:  0.5,
		ROIPVOXHoldTime:   2 * time.Second,
		ROIPMaxTXDuration: 30 * time.Second,
		ROIPInputDevice:   "hw:2",
	}
	cfg.applyDefaults()

	if cfg.ROIPCOSGPIOMask != 0x04 {
		t.Errorf("ROIPCOSGPIOMask overwritten; got %d", cfg.ROIPCOSGPIOMask)
	}

	if cfg.ROIPVOXThreshold != 0.5 {
		t.Errorf("ROIPVOXThreshold overwritten; got %f", cfg.ROIPVOXThreshold)
	}

	if cfg.ROIPVOXHoldTime != 2*time.Second {
		t.Errorf("ROIPVOXHoldTime overwritten; got %v", cfg.ROIPVOXHoldTime)
	}

	if cfg.ROIPMaxTXDuration != 30*time.Second {
		t.Errorf("ROIPMaxTXDuration overwritten; got %v", cfg.ROIPMaxTXDuration)
	}

	if cfg.ROIPInputDevice != "hw:2" {
		t.Errorf("ROIPInputDevice overwritten; got %q", cfg.ROIPInputDevice)
	}
}

// ─── EnableTalkGroupSend / Receive / GetTalkGroupStates tests ────────────────

func setupActiveConfigWithPorts(t *testing.T, n int) *CommsConfig {
	t.Helper()

	ports := make([]*portChannel, n)
	mcastPorts := make([]McastPortConfig, n)

	for i := 0; i < n; i++ {
		ports[i] = &portChannel{
			cfg: McastPortConfig{Address: "239.0.0.1", Port: 5004 + i*2},
		}
		ports[i].sendEnabled.Store(true)
		ports[i].receiveEnabled.Store(true)
		mcastPorts[i] = ports[i].cfg
	}

	cfg := &CommsConfig{
		Log:        zerolog.Nop(),
		McastPorts: mcastPorts,
		runtime:    &CommsRuntime{ports: ports},
	}

	SetDefault(cfg)
	t.Cleanup(func() { SetDefault(nil) })

	return cfg
}

func TestEnableTalkGroupSend_TogglesState(t *testing.T) {
	cfg := setupActiveConfigWithPorts(t, 2)

	if err := EnableTalkGroupSend(1, false); err != nil {
		t.Fatal(err)
	}

	if cfg.runtime.ports[1].sendEnabled.Load() {
		t.Error("send should be disabled")
	}

	if err := EnableTalkGroupSend(1, true); err != nil {
		t.Fatal(err)
	}

	if !cfg.runtime.ports[1].sendEnabled.Load() {
		t.Error("send should be enabled")
	}
}

func TestEnableTalkGroupReceive_TogglesState(t *testing.T) {
	cfg := setupActiveConfigWithPorts(t, 1)

	if err := EnableTalkGroupReceive(0, false); err != nil {
		t.Fatal(err)
	}

	if cfg.runtime.ports[0].receiveEnabled.Load() {
		t.Error("receive should be disabled")
	}
}

// ─── GetWebEventSource / GetWebAudioBridge tests ─────────────────────────────

func TestGetWebEventSource_NotRunning(t *testing.T) {
	SetDefault(nil)

	if got := GetWebEventSource(); got != nil {
		t.Errorf("expected nil when not running, got %v", got)
	}
}

func TestGetWebAudioBridge_NotRunning(t *testing.T) {
	SetDefault(nil)

	if got := GetWebAudioBridge(); got != nil {
		t.Errorf("expected nil when not running, got %v", got)
	}
}

func TestGetWebEventSource_ReturnsNilWhenNoWebSource(t *testing.T) {
	cfg := setupActiveConfigWithPorts(t, 1)
	cfg.runtime.webEvtSrc = nil

	if got := GetWebEventSource(); got != nil {
		t.Errorf("expected nil when web source not configured, got %v", got)
	}
}

func TestGetWebAudioBridge_ReturnsNilWhenNoBridge(t *testing.T) {
	cfg := setupActiveConfigWithPorts(t, 1)
	cfg.runtime.webBridge = nil

	if got := GetWebAudioBridge(); got != nil {
		t.Errorf("expected nil when bridge not configured, got %v", got)
	}
}
