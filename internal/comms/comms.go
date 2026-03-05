package comms

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/config"
)

// ─── Package-level constants ──────────────────────────────────────────────────

const (
	sampleRate        int    = 48000
	channels          int    = 1
	frameSize         int    = 960 // 20 ms at 48 kHz
	targetBitrate     int    = 32000
	encoderComplexity int    = 10
	packetLossPerc    int    = 30
	defaultKey        string = "any"
	defaultIface      string = "br-ahwlan"
	defaultCommDevice string = "/dev/hidraw0/*"
	defaultCommName   string = "AllInOneCable"
	defaultCtrlSrc    string = "cm108"

	// encBufSize is the maximum Opus encode output buffer. 1450 bytes matches
	// the UDP MTU and is far larger than typical Opus output (~80–160 B at
	// 32 kbps).
	encBufSize = 1450
)

// ─── Buffer pools ─────────────────────────────────────────────────────────────
//
// Hot-path audio callbacks and the playout loop allocate fixed-size slices
// every 20 ms. Pooling them eliminates per-frame GC pressure.

var (
	int16Pool = sync.Pool{ //nolint:gochecknoglobals
		New: func() any {
			s := make([]int16, frameSize)

			return &s
		},
	}
	float32Pool = sync.Pool{ //nolint:gochecknoglobals
		New: func() any {
			s := make([]float32, frameSize)

			return &s
		},
	}
	encBufPool = sync.Pool{ //nolint:gochecknoglobals
		New: func() any {
			s := make([]byte, encBufSize)

			return &s
		},
	}
)

// returnFloat32 returns a pooled []float32 slice to float32Pool.
// Non-pooled slices (e.g. beep buffers) are silently ignored because their
// capacity will differ from frameSize.
func returnFloat32(s []float32) {
	if cap(s) != frameSize {
		return // not from the pool (beep buffers, etc.)
	}

	sp := &s
	float32Pool.Put(sp)
}

// activeConfig holds the CommsConfig most recently started via Start().
// UpdateMulticastEndpoint reads it so callers need not pass the config explicitly.
var activeConfig atomic.Pointer[CommsConfig] //nolint:gochecknoglobals

// ─── McastPortConfig / McastPortState ────────────────────────────────────────

// McastPortConfig describes a single multicast endpoint that the comms
// subsystem listens and/or transmits on. Ports with Send=false will not open
// an RTP/RTCP sender; ports with Receive=false will not open an RTP receiver
// socket.
type McastPortConfig struct {
	Address string
	Port    int
	Send    bool
	Receive bool
}

// McastPortState is a read-only snapshot of the runtime direction-toggle state
// for a single port. Returned by GetTalkGroupStates.
type McastPortState struct {
	Address        string
	Port           int
	SendEnabled    bool
	ReceiveEnabled bool
}

// ─── portChannel ─────────────────────────────────────────────────────────────

// portChannel holds all live resources for one McastPortConfig entry.
// sendEnabled and receiveEnabled are atomic bools that can be toggled at
// runtime via EnableTalkGroupSend / EnableTalkGroupReceive without restarting any goroutine
// or socket.
type portChannel struct {
	rtpSess               rtpSender
	playbackStream        AudioStream
	sender                *swappableSender
	rtcpSend              *swappableSender
	receiver              *swappableReceiver
	playbackBuffer        chan []float32
	cfg                   McastPortConfig
	playbackHighWaterMark int
	lastRemoteRx          atomic.Int64
	playbackDrops         atomic.Int64
	sendEnabled           atomic.Bool
	receiveEnabled        atomic.Bool
}

// ─── CommsRuntime ─────────────────────────────────────────────────────────────

// CommsRuntime holds live resources allocated by Start. All audio/network
// fields are interfaces so that unit tests can inject fakes without hardware.
type CommsRuntime struct {
	decoder         AudioDecoder
	localIP         atomic.Pointer[string]
	encoder         AudioEncoder
	broadcastStream AudioStream
	ports           []*portChannel
	reopenBroadcast func() error
	beepBufferStart []float32
	beepBufferStop  []float32
	broadcasting    atomic.Bool
}

// ─── CommsConfig ──────────────────────────────────────────────────────────────

// CommsConfig holds the static configuration for the comms subsystem.
// Allocate one with NewComms and call Start to begin operation.
// All exported fields must be set before Start is called.
type CommsConfig struct {
	Log                      zerolog.Logger
	Interrupt                chan os.Signal
	runtime                  *CommsRuntime
	RtpID                    string
	CommKey                  string
	Iface                    string
	NanoPTTDevicePath        string
	BluetoothInputDevice     string
	BluetoothOutputDevice    string
	BluetoothAudioDeviceHint string
	ControlSource            string
	NanoPTTDeviceName        string
	McastPorts               []McastPortConfig
	PlaybackDepth            int
	MicGain                  float32
	EnableNanoPTT            bool
	Debug                    bool
	Loopback                 bool
	Trace                    bool
	Enable                   bool
	EnableBluetoothPtt       bool
}

// NewComms copies cfg and returns a pointer ready for Start.
func NewComms(cfg CommsConfig) *CommsConfig {
	mcastPorts := make([]McastPortConfig, len(cfg.McastPorts))
	copy(mcastPorts, cfg.McastPorts)

	return &CommsConfig{
		Log:                      cfg.Log,
		Interrupt:                cfg.Interrupt,
		Enable:                   cfg.Enable,
		Iface:                    cfg.Iface,
		McastPorts:               mcastPorts,
		CommKey:                  cfg.CommKey,
		RtpID:                    cfg.RtpID,
		Debug:                    cfg.Debug,
		Loopback:                 cfg.Loopback,
		Trace:                    cfg.Trace,
		ControlSource:            cfg.ControlSource,
		MicGain:                  cfg.MicGain,
		EnableNanoPTT:            cfg.EnableNanoPTT,
		NanoPTTDevicePath:        cfg.NanoPTTDevicePath,
		NanoPTTDeviceName:        cfg.NanoPTTDeviceName,
		EnableBluetoothPtt:       cfg.EnableBluetoothPtt,
		BluetoothAudioDeviceHint: cfg.BluetoothAudioDeviceHint,
		BluetoothInputDevice:     cfg.BluetoothInputDevice,
		BluetoothOutputDevice:    cfg.BluetoothOutputDevice,
		PlaybackDepth:            cfg.PlaybackDepth,
	}
}

// ─── applyDefaults ────────────────────────────────────────────────────────────

func (cfg *CommsConfig) applyDefaults() {
	if cfg.Iface == "" {
		cfg.Iface = defaultIface
	}

	if len(cfg.McastPorts) == 0 {
		tgs := config.GetMulticastTalkGroups()
		cfg.McastPorts = make([]McastPortConfig, len(tgs))
		for i, tg := range tgs {
			cfg.McastPorts[i] = McastPortConfig{
				Address: tg.Address,
				Port:    tg.Port,
				Send:    i == 0,
				Receive: i == 0,
			}
		}
	}

	if cfg.CommKey == "" {
		cfg.CommKey = defaultKey
	}

	if cfg.NanoPTTDevicePath == "" {
		cfg.NanoPTTDevicePath = defaultCommDevice
	}

	if cfg.NanoPTTDeviceName == "" {
		cfg.NanoPTTDeviceName = defaultCommName
	}

	cfg.ControlSource = normalizeControlSource(cfg.ControlSource)

	if cfg.RtpID == "" {
		if hostname, err := os.Hostname(); err == nil && hostname != "" {
			cfg.RtpID = hostname
		}
	}

	if cfg.BluetoothAudioDeviceHint != "" {
		if cfg.BluetoothInputDevice == "" {
			cfg.BluetoothInputDevice = cfg.BluetoothAudioDeviceHint
		}

		if cfg.BluetoothOutputDevice == "" {
			cfg.BluetoothOutputDevice = cfg.BluetoothAudioDeviceHint
		}
	}
}

// ─── buildCodec ───────────────────────────────────────────────────────────────

func (cfg *CommsConfig) buildCodec() (AudioEncoder, AudioDecoder, error) {
	enc, err := newOpusEncoder()
	if err != nil {
		return nil, nil, err
	}

	dec, err := newOpusDecoder()
	if err != nil {
		return nil, nil, err
	}

	return enc, dec, nil
}

// ─── buildNetwork ─────────────────────────────────────────────────────────────

// listenRTPReceiver opens a UDP socket bound to addr with SO_REUSEPORT enabled.
//
// SO_REUSEPORT lets a second socket bind to the same port while the current
// receiver is still open. This is required for UpdateMulticastEndpoint when
// the port does not change: buildSinglePortChannel must be able to acquire
// the new socket before the old one is closed.
func listenRTPReceiver(addr *net.UDPAddr) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
			})
		},
	}

	pc, err := lc.ListenPacket(context.Background(), "udp4", addr.String())
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()

		return nil, errors.New("listenRTPReceiver: unexpected PacketConn type")
	}

	return conn, nil
}

// buildSinglePortChannel opens all sockets and creates the RTP session for one
// McastPortConfig entry. Directions (Send / Receive) are reflected by which
// fields are populated: a Send=false port has nil sender/rtcpSend/rtpSess; a
// Receive=false port has nil receiver. The atomic direction flags are
// initialized from mpc.Send / mpc.Receive so the hot paths can read them
// without locking.
func (cfg *CommsConfig) buildSinglePortChannel( //nolint:gocognit
	mpc McastPortConfig,
	localIP string,
	ifi *net.Interface,
	ssrc uint32,
) (*portChannel, error) {
	pc := &portChannel{cfg: mpc}
	pc.sendEnabled.Store(mpc.Send)
	pc.receiveEnabled.Store(mpc.Receive)

	if mpc.Send {
		// ── RTP sender ─────────────────────────────────────────────────
		dst := &net.UDPAddr{IP: net.ParseIP(mpc.Address), Port: mpc.Port}
		src := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		sendConn, err := net.DialUDP("udp4", src, dst)
		if err != nil {
			return nil, fmt.Errorf("dial RTP sender %s:%d: %w", mpc.Address, mpc.Port, err)
		}

		// ── RTCP sender ────────────────────────────────────────────────
		rtcpDst := &net.UDPAddr{IP: net.ParseIP(mpc.Address), Port: mpc.Port + 1}
		rtcpSrc := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		rtcpConn, err := net.DialUDP("udp4", rtcpSrc, rtcpDst)
		if err != nil {
			_ = sendConn.Close()

			return nil, fmt.Errorf("dial RTCP sender %s:%d: %w", mpc.Address, mpc.Port+1, err)
		}

		sender := newSwappableSender(sendConn)
		rtcpSend := newSwappableSender(rtcpConn)

		sess, err := newPionRTPSession(ssrc, sender, rtcpSend, cfg.Log)
		if err != nil {
			_ = sendConn.Close()
			_ = rtcpConn.Close()

			return nil, fmt.Errorf("pion RTP session for %s:%d: %w", mpc.Address, mpc.Port, err)
		}

		pc.sender = sender
		pc.rtcpSend = rtcpSend
		pc.rtpSess = sess

		cfg.Log.Debug().Msgf("comms: RTP sender %s:%d  RTCP %s:%d", mpc.Address, mpc.Port, mpc.Address, mpc.Port+1)
	}

	if mpc.Receive { //nolint:nestif
		// ── RTP receiver ────────────────────────────────────────────────
		// SO_REUSEPORT lets UpdateMulticastEndpoint open a replacement socket
		// on the same port while the current receiver is still running.
		recvConn, err := listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: mpc.Port})
		if err != nil {
			if pc.sender != nil {
				_ = pc.sender.Close()

				_ = pc.rtcpSend.Close()
				if s, ok := pc.rtpSess.(*pionRTPSession); ok {
					_ = s.close()
				}
			}

			return nil, fmt.Errorf("listen RTP receiver %s:%d: %w", mpc.Address, mpc.Port, err)
		}

		if err := recvConn.SetReadBuffer(65535); err != nil {
			_ = recvConn.Close()

			if pc.sender != nil {
				_ = pc.sender.Close()

				_ = pc.rtcpSend.Close()
				if s, ok := pc.rtpSess.(*pionRTPSession); ok {
					_ = s.close()
				}
			}

			return nil, fmt.Errorf("set RTP read buffer: %w", err)
		}

		if err := joinMulticastGroup(ifi, recvConn, net.ParseIP(mpc.Address)); err != nil {
			_ = recvConn.Close()

			if pc.sender != nil {
				_ = pc.sender.Close()

				_ = pc.rtcpSend.Close()
				if s, ok := pc.rtpSess.(*pionRTPSession); ok {
					_ = s.close()
				}
			}

			return nil, err
		}

		pc.receiver = newSwappableReceiver(recvConn)

		cfg.Log.Debug().Msgf("comms: RTP receiver port %d", mpc.Port)
	}

	return pc, nil
}

// buildNetwork opens sockets for every McastPortConfig entry and returns the
// assembled portChannel slice plus the local IP address of cfg.Iface.
//
// The SSRC used for all Send-enabled ports is derived from cfg.RtpID (or
// localIP as fallback), keeping transmissions from this node identifiable
// across talk groups.
func (cfg *CommsConfig) buildNetwork() ([]*portChannel, string, error) {
	localIP, ifi, err := getIfaceIPv4(cfg.Iface)
	if err != nil {
		return nil, "", err
	}

	cfg.Log.Debug().Msgf("comms: interface %s localIP %s", cfg.Iface, localIP)

	rtpID := cfg.RtpID
	if rtpID == "" {
		rtpID = localIP
	}

	ssrc := ssrcFromID(rtpID)

	ports := make([]*portChannel, 0, len(cfg.McastPorts))

	for _, mpc := range cfg.McastPorts {
		pc, err := cfg.buildSinglePortChannel(mpc, localIP, ifi, ssrc)
		if err != nil {
			// Clean up already-built channels before propagating the error.
			for _, built := range ports {
				if built.receiver != nil {
					_ = built.receiver.Close()
				}

				if built.sender != nil {
					_ = built.sender.Close()
					_ = built.rtcpSend.Close()
				}

				if s, ok := built.rtpSess.(*pionRTPSession); ok && built.rtpSess != nil {
					_ = s.close()
				}
			}

			return nil, "", err
		}

		ports = append(ports, pc)
	}

	return ports, localIP, nil
}

// ─── buildAudio ───────────────────────────────────────────────────────────────

// buildAudio resolves PortAudio devices, opens a dedicated playback stream for
// every Receive-capable port (storing it in portChannel.playbackStream), and
// opens the shared broadcast capture stream. Per-port playback streams are
// accessible via rt.ports after this call returns.
func (cfg *CommsConfig) buildAudio(rt *CommsRuntime) (
	broadcast AudioStream,
	inDev *portaudio.DeviceInfo,
	err error,
) {
	outDev, err := resolveAudioDevice(cfg.BluetoothOutputDevice, false)
	if err != nil {
		return nil, nil, err
	}

	inDev, err = resolveAudioDevice(cfg.BluetoothInputDevice, true)
	if err != nil {
		return nil, nil, err
	}

	cfg.Log.Info().Msgf("comms: audio in=%s out=%s", inDev.Name, outDev.Name)

	playbackDepth := 50
	if cfg.PlaybackDepth > 0 {
		playbackDepth = cfg.PlaybackDepth
	}

	playbackParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outDev,
			Channels: channels,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	// Open a dedicated playback stream for every Receive-capable port.
	for _, pc := range rt.ports {
		if !pc.cfg.Receive {
			continue
		}

		pc.playbackBuffer = make(chan []float32, playbackDepth)
		pc.playbackHighWaterMark = cap(pc.playbackBuffer) * 3 / 4

		pcRef := pc // capture for callback closure

		rawPlayback, openErr := portaudio.OpenStream(playbackParams, func(_, out []float32) {
			select {
			case data := <-pcRef.playbackBuffer:
				copy(out, data)
				returnFloat32(data)
			default:
				for i := range out {
					out[i] = 0
				}
			}
		})
		if openErr != nil {
			// Close already-opened per-port streams before propagating error.
			for _, built := range rt.ports {
				if built.playbackStream != nil {
					_ = built.playbackStream.Close()
					built.playbackStream = nil
				}
			}

			return nil, nil, fmt.Errorf("open playback stream for port %d: %w", pc.cfg.Port, openErr)
		}

		pc.playbackStream = &portaudioStream{rawPlayback}
	}

	broadcast, err = cfg.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		for _, pc := range rt.ports {
			if pc.playbackStream != nil {
				_ = pc.playbackStream.Close()
				pc.playbackStream = nil
			}
		}

		return nil, nil, err
	}

	return broadcast, inDev, nil
}

// sendToAllPorts sends an encoded RTP payload to every port where sendEnabled
// is true and an rtpSess is configured. Send errors are logged at Debug level
// and do not abort remaining ports.
func (cfg *CommsConfig) sendToAllPorts(rt *CommsRuntime, payload []byte) {
	for _, pc := range rt.ports {
		if !pc.sendEnabled.Load() || pc.rtpSess == nil {
			continue
		}

		if err := pc.rtpSess.send(payload); err != nil {
			cfg.Log.Debug().Err(err).
				Str("addr", pc.cfg.Address).
				Int("port", pc.cfg.Port).
				Msg("comms: RTP send failed")
		}
	}
}

// openBroadcastStreamOn creates a PortAudio capture stream that encodes mic
// audio via Opus and transmits it as RTP to all send-enabled ports via sendToAllPorts.
func (cfg *CommsConfig) openBroadcastStreamOn(inDev *portaudio.DeviceInfo, rt *CommsRuntime) (AudioStream, error) {
	inParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inDev,
			Channels: channels,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	stream, err := portaudio.OpenStream(inParams, func(in []float32) {
		gain := cfg.MicGain
		if gain <= 0 {
			gain = 1.0
		}

		pcmPtr := int16Pool.Get().(*[]int16) //nolint:forcetypeassert
		pcm := (*pcmPtr)[:len(in)]

		for i, v := range in {
			v *= gain
			if v > 1.0 {
				v = 1.0
			} else if v < -1.0 {
				v = -1.0
			}

			pcm[i] = int16(v * 32767)
		}

		bufPtr := encBufPool.Get().(*[]byte) //nolint:forcetypeassert
		buf := *bufPtr

		n, encErr := rt.encoder.Encode(pcm, buf)

		int16Pool.Put(pcmPtr)

		if encErr != nil {
			encBufPool.Put(bufPtr)

			return
		}

		cfg.sendToAllPorts(rt, buf[:n])

		encBufPool.Put(bufPtr)

		if cfg.Trace {
			cfg.Log.Trace().Int("encoded_bytes", n).Msg("comms: multicast packet sent")
		}
	})
	if err != nil {
		return nil, fmt.Errorf("open broadcast stream: %w", err)
	}

	return &portaudioStream{stream}, nil
}

// reopenBroadcastStream closes the current broadcast stream and opens a new one.
func (cfg *CommsConfig) reopenBroadcastStream(rt *CommsRuntime, inDev *portaudio.DeviceInfo) error {
	if inDev == nil {
		return errors.New("input device is not set")
	}

	if rt.broadcastStream != nil {
		_ = rt.broadcastStream.Close()
		rt.broadcastStream = nil
	}

	stream, err := cfg.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		return err
	}

	rt.broadcastStream = stream

	return nil
}

// ─── buildEventSource ─────────────────────────────────────────────────────────

func (cfg *CommsConfig) buildEventSource() (EventSource, error) {
	switch cfg.ControlSource {
	case defaultCtrlSrc:
		cfg.Log.Info().Msgf("comms: PTT on CM108 HID dongle (VID=0x%04X PID=0x%04X)",
			cm108VendorID, cm108ProductID)

		return NewCM108Source(cfg.Log), nil

	default:
		dev := cfg.findCommDevice()
		if dev == nil {
			return nil, errors.New("comms: PTT device not found")
		}

		cfg.Log.Info().Msgf("comms: PTT on evdev device: %s", dev.Name)

		return NewNanoPTTSource(dev, cfg.CommKey, cfg.Log), nil
	}
}

// ─── replaceNetwork ───────────────────────────────────────────────────────────

// replaceNetwork atomically swaps the packet-level I/O connections for port 0
// and closes the old connections. newSender, newRTCPSender, or newReceiver may
// be nil when that direction is not applicable to the port. Closing the old
// receiver unblocks any in-flight ReadFromUDP in receiveLoop.
func (cfg *CommsConfig) replaceNetwork(
	rt *CommsRuntime,
	newSender PacketWriter,
	newRTCPSender PacketWriter,
	newReceiver PacketReader,
	newLocalIP string,
) {
	pc := rt.ports[0]

	if pc.receiver != nil && newReceiver != nil {
		old := pc.receiver.swap(newReceiver)
		_ = old.Close()
	}

	if pc.sender != nil && newSender != nil {
		old := pc.sender.swap(newSender)
		if c, ok := old.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}

	if pc.rtcpSend != nil && newRTCPSender != nil {
		old := pc.rtcpSend.swap(newRTCPSender)
		if c, ok := old.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}

	rt.localIP.Store(&newLocalIP)
}

// ─── UpdateMulticastEndpoint ──────────────────────────────────────────────────

// GetActiveMulticastAddr returns the multicast group address of the first
// configured port in the live comms subsystem. Returns an empty string if
// comms has not been started or no ports are configured.
func GetActiveMulticastAddr() string {
	cfg := activeConfig.Load()
	if cfg == nil || len(cfg.McastPorts) == 0 {
		return ""
	}

	return cfg.McastPorts[0].Address
}

// GetActiveMulticastPort returns the UDP port of the first configured port in
// the live comms subsystem. Returns 0 if comms has not been started or no
// ports are configured.
func GetActiveMulticastPort() int {
	cfg := activeConfig.Load()
	if cfg == nil || len(cfg.McastPorts) == 0 {
		return 0
	}

	return cfg.McastPorts[0].Port
}

// UpdateMulticastEndpoint changes the multicast group address and UDP port
// used by the live comms subsystem at runtime. It is only supported when
// exactly one McastPort is configured; use EnableTalkGroupSend / EnableTalkGroupReceive for
// multi-port control.
//
// New sockets are opened before the swap, so the subsystem is never left
// without functional sockets on error.
func UpdateMulticastEndpoint(addr string, port int) error { //nolint:gocognit
	cfg := activeConfig.Load()
	if cfg == nil || cfg.runtime == nil {
		return errors.New("comms: subsystem is not running")
	}

	if len(cfg.McastPorts) > 1 {
		return errors.New("comms: UpdateMulticastEndpoint is not supported when more than one McastPort is configured")
	}

	rt := cfg.runtime

	ip := net.ParseIP(addr)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("comms: %q is not a valid IPv4 address", addr)
	}

	if !ip.IsMulticast() {
		return fmt.Errorf("comms: %q is not a multicast address", addr)
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("comms: port %d is out of range [1, 65535]", port)
	}

	localIP, ifi, err := getIfaceIPv4(cfg.Iface)
	if err != nil {
		return fmt.Errorf("comms: failed to get interface IP: %w", err)
	}

	oldMPC := cfg.McastPorts[0]

	// Open new raw sockets for the destination. Keep variables typed as
	// *net.UDPConn so nil comparisons work correctly.
	var sendConn, rtcpConn, recvConn *net.UDPConn

	if oldMPC.Send {
		dst := &net.UDPAddr{IP: net.ParseIP(addr), Port: port}
		src := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		sendConn, err = net.DialUDP("udp4", src, dst)
		if err != nil {
			return fmt.Errorf("comms: dial RTP sender %s:%d: %w", addr, port, err)
		}

		rtcpDst := &net.UDPAddr{IP: net.ParseIP(addr), Port: port + 1}
		rtcpSrc := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		rtcpConn, err = net.DialUDP("udp4", rtcpSrc, rtcpDst)
		if err != nil {
			_ = sendConn.Close()

			return fmt.Errorf("comms: dial RTCP sender %s:%d: %w", addr, port+1, err)
		}
	}

	if oldMPC.Receive { //nolint:nestif
		recvConn, err = listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: port})
		if err != nil {
			if sendConn != nil {
				_ = sendConn.Close()
				_ = rtcpConn.Close()
			}

			return fmt.Errorf("comms: listen on port %d: %w", port, err)
		}

		if setErr := recvConn.SetReadBuffer(65535); setErr != nil {
			_ = recvConn.Close()

			if sendConn != nil {
				_ = sendConn.Close()
				_ = rtcpConn.Close()
			}

			return fmt.Errorf("comms: set read buffer: %w", setErr)
		}

		if joinErr := joinMulticastGroup(ifi, recvConn, net.ParseIP(addr)); joinErr != nil {
			_ = recvConn.Close()

			if sendConn != nil {
				_ = sendConn.Close()
				_ = rtcpConn.Close()
			}

			return joinErr
		}
	}

	// Swap the raw connections into the live swappable wrappers. Interface
	// conversions are only done when the connection is non-nil so we never
	// pass a typed-nil PacketWriter/PacketReader (which would not compare == nil).
	var (
		newSend, newRTCP PacketWriter
		newRecv          PacketReader
	)

	if sendConn != nil {
		newSend = sendConn
		newRTCP = rtcpConn
	}

	if recvConn != nil {
		newRecv = recvConn
	}

	cfg.replaceNetwork(rt, newSend, newRTCP, newRecv, localIP)

	cfg.McastPorts[0] = McastPortConfig{
		Address: addr,
		Port:    port,
		Send:    oldMPC.Send,
		Receive: oldMPC.Receive,
	}

	cfg.Log.Info().Msgf("comms: multicast endpoint updated %s:%d → %s:%d",
		oldMPC.Address, oldMPC.Port, addr, port)

	return nil
}

// ─── Start ────────────────────────────────────────────────────────────────────

// EnableTalkGroupSend enables or disables RTP transmission on the port at the given
// zero-based index. It is safe to call concurrently with the send path.
// Returns an error when comms is not running or portIdx is out of range.
func EnableTalkGroupSend(portIdx int, enabled bool) error {
	cfg := activeConfig.Load()
	if cfg == nil || cfg.runtime == nil {
		return errors.New("comms: subsystem is not running")
	}

	rt := cfg.runtime
	if portIdx < 0 || portIdx >= len(rt.ports) {
		return fmt.Errorf("comms: port index %d out of range [0, %d)", portIdx, len(rt.ports))
	}

	rt.ports[portIdx].sendEnabled.Store(enabled)

	return nil
}

// EnableTalkGroupReceive enables or disables RTP reception on the port at the given
// zero-based index. It is safe to call concurrently with the receive path.
// Returns an error when comms is not running or portIdx is out of range.
func EnableTalkGroupReceive(portIdx int, enabled bool) error {
	cfg := activeConfig.Load()
	if cfg == nil || cfg.runtime == nil {
		return errors.New("comms: subsystem is not running")
	}

	rt := cfg.runtime
	if portIdx < 0 || portIdx >= len(rt.ports) {
		return fmt.Errorf("comms: port index %d out of range [0, %d)", portIdx, len(rt.ports))
	}

	rt.ports[portIdx].receiveEnabled.Store(enabled)

	return nil
}

// GetTalkGroupStates returns a snapshot of the runtime direction-toggle state for
// all configured ports. Returns an error when comms is not running.
func GetTalkGroupStates() ([]McastPortState, error) {
	cfg := activeConfig.Load()
	if cfg == nil || cfg.runtime == nil {
		return nil, errors.New("comms: subsystem is not running")
	}

	rt := cfg.runtime
	states := make([]McastPortState, len(rt.ports))

	for i, pc := range rt.ports {
		states[i] = McastPortState{
			Address:        pc.cfg.Address,
			Port:           pc.cfg.Port,
			SendEnabled:    pc.sendEnabled.Load(),
			ReceiveEnabled: pc.receiveEnabled.Load(),
		}
	}

	return states, nil
}

// Start initializes all comms subsystems and blocks until an OS interrupt is
// received. Returns immediately if Enable is false.
func (cfg *CommsConfig) Start() {
	if !cfg.Enable {
		cfg.Log.Info().Msg("comms: functionality disabled; not starting")

		return
	}

	// Voice comms is not supported on MIPS due to lack of audio hardware
	if runtime.GOARCH == "mipsle" {
		cfg.Log.Error().Msg("comms: running on MIPS; audio quality may be poor due to lack of hardware FPU")

		return
	}

	cfg.applyDefaults()

	if cfg.ControlSource == defaultCtrlSrc {
		detectAndSetALSACard(cfg.Log)
	}

	switch {
	case cfg.Trace:
		cfg.Log = cfg.Log.Level(zerolog.TraceLevel)
	case cfg.Debug:
		cfg.Log = cfg.Log.Level(zerolog.DebugLevel)
	}

	if cfg.Debug {
		cfg.logInputDeviceList()
	}

	cfg.Log.Info().Msgf(
		"comms: starting iface=%s ports=%d key=%s debug=%t trace=%t loopback=%t device=%s ctrl=%s hint=%s",
		cfg.Iface, len(cfg.McastPorts), cfg.CommKey,
		cfg.Debug, cfg.Trace, cfg.Loopback, cfg.NanoPTTDeviceName, cfg.ControlSource, cfg.BluetoothAudioDeviceHint,
	)

	// ── codec ──────────────────────────────────────────────────────────────
	enc, dec, err := cfg.buildCodec()
	if err != nil {
		cfg.Log.Fatal().Err(err).Msg("comms: failed to build Opus codec")
	}

	// ── beep tones ─────────────────────────────────────────────────────────
	beepStart := make([]float32, frameSize)
	beepStop := make([]float32, frameSize)

	for i := range beepStart {
		beepStart[i] = float32(math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate))) * 0.2
		beepStop[i] = float32(math.Sin(2*math.Pi*600*float64(i)/float64(sampleRate))) * 0.2
	}

	// ── network ────────────────────────────────────────────────────────────
	ports, localIP, netErr := cfg.buildNetwork()
	if netErr != nil {
		cfg.Log.Fatal().Err(netErr).Msg("comms: failed to set up network")
	}

	// ── assemble runtime ───────────────────────────────────────────────────
	rt := &CommsRuntime{
		encoder:         enc,
		decoder:         dec,
		ports:           ports,
		beepBufferStart: beepStart,
		beepBufferStop:  beepStop,
	}

	rt.localIP.Store(&localIP)

	defer func() {
		for _, pc := range rt.ports {
			if pc.receiver != nil {
				_ = pc.receiver.Close()
			}

			if pc.rtpSess != nil {
				if s, ok := pc.rtpSess.(*pionRTPSession); ok {
					_ = s.close()
				}
			}
		}
	}()

	cfg.runtime = rt
	activeConfig.Store(cfg)

	// ── PortAudio ──────────────────────────────────────────────────────────
	silenceALSAProbeNoise()

	err = portaudio.Initialize()

	restoreALSAErrorHandler()

	if err != nil {
		cfg.Log.Fatal().Err(err).Msg("comms: failed to initialize PortAudio")
	}

	go func() {
		<-cfg.Interrupt
		cfg.Log.Info().Msg("comms: received shutdown signal, cleaning up PortAudio")

		_ = portaudio.Terminate()

		os.Exit(0)
	}()

	broadcastStream, inDev, audioErr := cfg.buildAudio(rt)
	if audioErr != nil {
		cfg.Log.Fatal().Err(audioErr).Msg("comms: failed to build audio streams")
	}

	rt.broadcastStream = broadcastStream
	rt.reopenBroadcast = func() error { return cfg.reopenBroadcastStream(rt, inDev) }

	for _, pc := range rt.ports {
		if pc.playbackStream != nil {
			if startErr := pc.playbackStream.Start(); startErr != nil {
				cfg.Log.Fatal().Err(startErr).Msg("comms: failed to start playback stream")
			}

			defer func(s AudioStream) {
				_ = s.Stop()
				_ = s.Close()
			}(pc.playbackStream)
		}
	}

	defer broadcastStream.Close() //nolint:errcheck

	// ── event source ───────────────────────────────────────────────────────
	src, srcErr := cfg.buildEventSource()
	if srcErr != nil {
		cfg.Log.Fatal().Err(srcErr).Msg("comms: failed to build event source")
	}

	// ── run loop ───────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		cfg.Log.Info().Msg("comms: exiting service")
		cancel()
	}()

	cfg.Run(ctx, rt, src)
}
