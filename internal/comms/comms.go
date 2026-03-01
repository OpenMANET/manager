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

	DefaultCommsPort int = 5007

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

// ─── CommsRuntime ─────────────────────────────────────────────────────────────

// CommsRuntime holds live resources allocated by Start. All audio/network
// fields are interfaces so that unit tests can inject fakes without hardware.
type CommsRuntime struct {
	decoder         AudioDecoder
	localIP         atomic.Value // string
	encoder         AudioEncoder
	broadcastStream AudioStream
	playbackBuffer  chan []float32
	sender          *swappableSender
	receiver        *swappableReceiver
	rtpSess         rtpSender
	reopenBroadcast func() error
	beepBufferStart []float32
	beepBufferStop  []float32
	broadcasting    atomic.Bool
	lastRemoteRx    atomic.Int64 // UnixNano of last received remote RTP packet
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
	McastAddr                string
	McastPort                int
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
	return &CommsConfig{
		Log:                      cfg.Log,
		Interrupt:                cfg.Interrupt,
		Enable:                   cfg.Enable,
		Iface:                    cfg.Iface,
		McastAddr:                cfg.McastAddr,
		McastPort:                cfg.McastPort,
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

	if cfg.McastAddr == "" {
		cfg.McastAddr = config.GetMulticastTalkGroupAddresses()[0]
	}

	if cfg.McastPort == 0 {
		cfg.McastPort = DefaultCommsPort
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
// the port does not change: buildNetwork must be able to acquire the new socket
// before replaceNetwork closes the old one, preserving the invariant that the
// subsystem is never without a functional receive socket on error.
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
		return nil, err
	}

	conn, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close()

		return nil, errors.New("listenRTPReceiver: unexpected PacketConn type")
	}

	return conn, nil
}

// buildNetwork opens the RTP UDP sender/receiver and an RTCP sender.
// RTP is on McastPort; RTCP is on McastPort+1 (standard RTP port-pairing).
func (cfg *CommsConfig) buildNetwork() (
	rtpSend PacketWriter,
	rtpRecv PacketReader,
	rtcpSend PacketWriter,
	localIP string,
	err error,
) {
	localIP, ifi, err := getIfaceIPv4(cfg.Iface)
	if err != nil {
		return nil, nil, nil, "", err
	}

	cfg.Log.Debug().Msgf("comms: interface %s localIP %s", cfg.Iface, localIP)

	// ── RTP sender (unicast dial to multicast dst) ──────────────────────────
	dst := &net.UDPAddr{IP: net.ParseIP(cfg.McastAddr), Port: cfg.McastPort}
	src := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

	sendConn, dialErr := net.DialUDP("udp4", src, dst)
	if dialErr != nil {
		return nil, nil, nil, "", fmt.Errorf("dial RTP sender: %w", dialErr)
	}

	// ── RTP receiver ───────────────────────────────────────────────────────
	// SO_REUSEPORT is set so that UpdateMulticastEndpoint can open a replacement
	// socket on the same port while the current one is still running.
	recvConn, listenErr := listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: cfg.McastPort})
	if listenErr != nil {
		_ = sendConn.Close()

		return nil, nil, nil, "", fmt.Errorf("listen RTP receiver: %w", listenErr)
	}

	if err := recvConn.SetReadBuffer(65535); err != nil {
		_ = sendConn.Close()
		_ = recvConn.Close()

		return nil, nil, nil, "", fmt.Errorf("set RTP read buffer: %w", err)
	}

	if err := joinMulticastGroup(ifi, recvConn, net.ParseIP(cfg.McastAddr)); err != nil {
		_ = sendConn.Close()
		_ = recvConn.Close()

		return nil, nil, nil, "", err
	}

	// ── RTCP sender ────────────────────────────────────────────────────────
	rtcpDst := &net.UDPAddr{IP: net.ParseIP(cfg.McastAddr), Port: cfg.McastPort + 1}
	rtcpSrc := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

	rtcpConn, rtcpErr := net.DialUDP("udp4", rtcpSrc, rtcpDst)
	if rtcpErr != nil {
		_ = sendConn.Close()
		_ = recvConn.Close()

		return nil, nil, nil, "", fmt.Errorf("dial RTCP sender: %w", rtcpErr)
	}

	cfg.Log.Debug().Msgf("comms: RTP %s:%d  RTCP %s:%d",
		cfg.McastAddr, cfg.McastPort, cfg.McastAddr, cfg.McastPort+1)

	return sendConn, recvConn, rtcpConn, localIP, nil
}

// ─── buildAudio ───────────────────────────────────────────────────────────────

func (cfg *CommsConfig) buildAudio(rt *CommsRuntime) (
	playback AudioStream,
	broadcast AudioStream,
	inDev *portaudio.DeviceInfo,
	err error,
) {
	outDev, err := resolveAudioDevice(cfg.BluetoothOutputDevice, false)
	if err != nil {
		return nil, nil, nil, err
	}

	inDev, err = resolveAudioDevice(cfg.BluetoothInputDevice, true)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg.Log.Info().Msgf("comms: audio in=%s out=%s", inDev.Name, outDev.Name)

	playbackParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outDev,
			Channels: channels,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	rawPlayback, err := portaudio.OpenStream(playbackParams, func(_, out []float32) {
		select {
		case data := <-rt.playbackBuffer:
			copy(out, data)
			returnFloat32(data)
		default:
			for i := range out {
				out[i] = 0
			}
		}
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open playback stream: %w", err)
	}

	broadcast, err = cfg.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		_ = rawPlayback.Close()

		return nil, nil, nil, err
	}

	return &portaudioStream{rawPlayback}, broadcast, inDev, nil
}

// openBroadcastStreamOn creates a PortAudio capture stream that encodes mic
// audio via Opus and transmits it as RTP using rt.rtpSess.
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

		if sendErr := rt.rtpSess.send(buf[:n]); sendErr != nil {
			cfg.Log.Debug().Err(sendErr).Msg("comms: RTP send failed in broadcast callback")
		}

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

// replaceNetwork atomically swaps the sender, receiver, and local IP stored in
// a running CommsRuntime, then closes the old sockets. Closing the old receiver
// unblocks any in-flight ReadFromUDP in receiveLoop.
func (cfg *CommsConfig) replaceNetwork(
	rt *CommsRuntime,
	newSender PacketWriter,
	newReceiver PacketReader,
	newLocalIP string,
) {
	oldReceiver := rt.receiver.swap(newReceiver)
	oldSender := rt.sender.swap(newSender)
	rt.localIP.Store(newLocalIP)

	_ = oldReceiver.Close()

	if c, ok := oldSender.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

// ─── UpdateMulticastEndpoint ──────────────────────────────────────────────────

// GetActiveMulticastAddr returns the current multicast group address in use by the
// live comms subsystem. Returns an empty string if comms has not been started.
func GetActiveMulticastAddr() string {
	cfg := activeConfig.Load()
	if cfg == nil {
		return ""
	}

	return cfg.McastAddr
}

// UpdateMulticastEndpoint changes the multicast group address and UDP port
// used by the live comms subsystem at runtime. It is safe to call concurrently
// with the send/receive path.
//
// The new sockets are opened (with SO_REUSEPORT on the receiver) before the
// swap, so the subsystem is never left without functional sockets on error.
// On error the old sockets are kept unchanged.
func UpdateMulticastEndpoint(addr string, port int) error {
	cfg := activeConfig.Load()
	if cfg == nil || cfg.runtime == nil {
		return errors.New("comms: subsystem is not running")
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

	oldAddr, oldPort := cfg.McastAddr, cfg.McastPort
	cfg.McastAddr, cfg.McastPort = addr, port

	newSender, newReceiver, _, newLocalIP, err := cfg.buildNetwork()
	if err != nil {
		cfg.McastAddr, cfg.McastPort = oldAddr, oldPort

		return fmt.Errorf("comms: failed to establish %s:%d: %w", addr, port, err)
	}

	cfg.replaceNetwork(rt, newSender, newReceiver, newLocalIP)

	cfg.Log.Info().Msgf("comms: multicast endpoint updated %s:%d → %s:%d",
		oldAddr, oldPort, addr, port)

	return nil
}

// ─── Start ────────────────────────────────────────────────────────────────────

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
		"comms: starting iface=%s mcast=%s:%d key=%s debug=%t trace=%t loopback=%t device=%s ctrl=%s hint=%s",
		cfg.Iface, cfg.McastAddr, cfg.McastPort, cfg.CommKey,
		cfg.Debug, cfg.Trace, cfg.Loopback, cfg.NanoPTTDeviceName, cfg.ControlSource, cfg.BluetoothAudioDeviceHint,
	)

	// ── codec ──────────────────────────────────────────────────────────────
	enc, dec, err := cfg.buildCodec()
	if err != nil {
		cfg.Log.Fatal().Err(err).Msg("comms: failed to build Opus codec")
	}

	// ── playback buffer + beep tones ───────────────────────────────────────
	playbackDepth := 10
	if cfg.PlaybackDepth > 0 {
		playbackDepth = cfg.PlaybackDepth
	}

	playbackBuf := make(chan []float32, playbackDepth)

	beepStart := make([]float32, frameSize)
	beepStop := make([]float32, frameSize)

	for i := range beepStart {
		beepStart[i] = float32(math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate))) * 0.2
		beepStop[i] = float32(math.Sin(2*math.Pi*600*float64(i)/float64(sampleRate))) * 0.2
	}

	// ── network ────────────────────────────────────────────────────────────
	rawSender, rawReceiver, rawRTCPSender, localIP, netErr := cfg.buildNetwork()
	if netErr != nil {
		cfg.Log.Fatal().Err(netErr).Msg("comms: failed to set up network")
	}

	// ── RTP session (pion) ─────────────────────────────────────────────────
	rtpID := cfg.RtpID
	if rtpID == "" {
		rtpID = localIP
	}

	sess, sessErr := newPionRTPSession(ssrcFromID(rtpID), rawSender, rawRTCPSender, cfg.Log)
	if sessErr != nil {
		cfg.Log.Fatal().Err(sessErr).Msg("comms: failed to create RTP session")
	}

	// ── assemble runtime ───────────────────────────────────────────────────
	rt := &CommsRuntime{
		encoder:         enc,
		decoder:         dec,
		sender:          newSwappableSender(rawSender),
		receiver:        newSwappableReceiver(rawReceiver),
		rtpSess:         sess,
		playbackBuffer:  playbackBuf,
		beepBufferStart: beepStart,
		beepBufferStop:  beepStop,
	}

	rt.localIP.Store(localIP)

	defer rt.receiver.Close()
	defer sess.close() //nolint:errcheck

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

	playbackStream, broadcastStream, inDev, audioErr := cfg.buildAudio(rt)
	if audioErr != nil {
		cfg.Log.Fatal().Err(audioErr).Msg("comms: failed to build audio streams")
	}

	rt.broadcastStream = broadcastStream
	rt.reopenBroadcast = func() error { return cfg.reopenBroadcastStream(rt, inDev) }

	if err := playbackStream.Start(); err != nil {
		cfg.Log.Fatal().Err(err).Msg("comms: failed to start playback stream")
	}

	defer func() { _ = playbackStream.Stop() }()
	defer playbackStream.Close() //nolint:errcheck
	defer broadcastStream.Close()

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
