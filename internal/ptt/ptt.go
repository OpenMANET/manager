package ptt

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"
)

/********* defaults *********/
const (
	sampleRate           int    = 48000
	channels             int    = 1
	frameSize            int    = 960
	targetBitrate        int    = 32000
	encoderComplexity    int    = 10
	packetLossPerc       int    = 30
	defaultKey           string = "any"
	defaultIface         string = "br-ahwlan"
	defaultG             string = "224.0.0.1"
	defaultPort          int    = 5007
	defaultProtocol      string = protocolUDP
	defaultDebug         bool   = true
	defaultLoopback      bool   = true
	defaultPTTDevice     string = "/dev/hidraw0/*"
	defaultPTTDeviceName string = "AllInOneCable"
	defaultControlSource string = "cm108"
)

// activeConfig holds the PTTConfig that was most recently started via Start().
// UpdateMulticastEndpoint reads it so callers need not pass the config explicitly.
var activeConfig atomic.Pointer[PTTConfig] //nolint:gochecknoglobals

// PTTRuntime holds live resources allocated by Start.  All fields are
// interfaces so that unit tests can inject fakes without touching hardware.
type PTTRuntime struct {
	decoder         AudioDecoder
	localIP         atomic.Value
	encoder         AudioEncoder
	broadcastStream AudioStream
	playbackBuffer  chan []float32
	sender          *swappableSender
	receiver        *swappableReceiver
	reopenBroadcast func() error
	beepBufferStart []float32
	beepBufferStop  []float32
	recordMutex     sync.Mutex
	rtpSSRC         uint32
	rtpSeq          uint16
	broadcasting    bool
}

// PTTConfig holds the static configuration for the PTT subsystem.
// Allocate one with NewPTT and call Start to begin operation.
// All exported fields must be set before Start is called; they are normalised
// in-place by applyDefaults() at startup.
type PTTConfig struct {
	Log             zerolog.Logger
	Interrupt       chan os.Signal
	runtime         *PTTRuntime
	ControlSource   string
	PTTKey          string
	Iface           string
	Protocol        string
	RtpID           string
	InputDevice     string
	OutputDevice    string
	AudioDeviceHint string
	PTTDeviceName   string
	McastAddr       string
	PTTDeviceGlob   string
	PlaybackDepth   int
	McastPort       int
	Debug           bool
	Loopback        bool
	Trace           bool
	Enable          bool
}

// NewPTT copies cfg and returns a pointer ready for Start.
func NewPTT(cfg PTTConfig) *PTTConfig {
	return &PTTConfig{
		Log:             cfg.Log,
		Interrupt:       cfg.Interrupt,
		Enable:          cfg.Enable,
		Iface:           cfg.Iface,
		McastAddr:       cfg.McastAddr,
		McastPort:       cfg.McastPort,
		PTTKey:          cfg.PTTKey,
		Protocol:        cfg.Protocol,
		RtpID:           cfg.RtpID,
		Debug:           cfg.Debug,
		Loopback:        cfg.Loopback,
		Trace:           cfg.Trace,
		PTTDeviceGlob:   cfg.PTTDeviceGlob,
		PTTDeviceName:   cfg.PTTDeviceName,
		ControlSource:   cfg.ControlSource,
		AudioDeviceHint: cfg.AudioDeviceHint,
		InputDevice:     cfg.InputDevice,
		OutputDevice:    cfg.OutputDevice,
		PlaybackDepth:   cfg.PlaybackDepth,
	}
}

// ─── Stage 1: defaults ────────────────────────────────────────────────────────

// applyDefaults fills any empty PTTConfig fields with package-level constants.
// It is idempotent and safe to call more than once.
func (ptt *PTTConfig) applyDefaults() {
	if ptt.Iface == "" {
		ptt.Iface = defaultIface
	}

	if ptt.McastAddr == "" {
		ptt.McastAddr = defaultG
	}

	if ptt.McastPort == 0 {
		ptt.McastPort = defaultPort
	}

	if ptt.PTTKey == "" {
		ptt.PTTKey = defaultKey
	}

	ptt.Protocol = normalizeProtocol(ptt.Protocol) // handles empty → "udp"
	if ptt.PTTDeviceGlob == "" {
		ptt.PTTDeviceGlob = defaultPTTDevice
	}

	if ptt.PTTDeviceName == "" {
		ptt.PTTDeviceName = defaultPTTDeviceName
	}

	ptt.ControlSource = normalizeControlSource(ptt.ControlSource) // handles empty → "cm108"

	// Resolve RtpID: explicit value → hostname
	if ptt.RtpID == "" {
		if hostname, err := os.Hostname(); err == nil && hostname != "" {
			ptt.RtpID = hostname
			ptt.Log.Debug().Msgf("RTP ID not set; using hostname %q", hostname)
		}
	}

	// AudioDeviceHint fills InputDevice / OutputDevice when both are empty.
	if ptt.AudioDeviceHint != "" {
		if ptt.InputDevice == "" {
			ptt.InputDevice = ptt.AudioDeviceHint
		}

		if ptt.OutputDevice == "" {
			ptt.OutputDevice = ptt.AudioDeviceHint
		}
	}
}

// ─── Stage 2: codec ───────────────────────────────────────────────────────────

// buildCodec creates the Opus encoder and decoder, returning them as the
// AudioEncoder / AudioDecoder interfaces.  Returns an error instead of
// calling log.Fatal so that test code and callers can handle failures.
func (ptt *PTTConfig) buildCodec() (AudioEncoder, AudioDecoder, error) {
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

// ─── Stage 3: network ─────────────────────────────────────────────────────────

// buildNetwork resolves the interface IP, dials the UDP sender, opens the UDP
// receiver, and joins the multicast group.
func (ptt *PTTConfig) buildNetwork() (PacketWriter, PacketReader, string, error) {
	localIP, ifi, err := ptt.getIfaceIPv4(ptt.Iface)
	if err != nil {
		return nil, nil, "", err
	}

	ptt.Log.Debug().Msgf("Using interface %s with IP %s", ptt.Iface, localIP)

	dst := &net.UDPAddr{IP: net.ParseIP(ptt.McastAddr), Port: ptt.McastPort}
	src := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

	sendConn, err := net.DialUDP("udp4", src, dst)
	if err != nil {
		return nil, nil, "", fmt.Errorf("dial UDP sender: %w", err)
	}

	ptt.Log.Debug().Msgf("Sender bound to %s -> %s:%d", localIP, ptt.McastAddr, ptt.McastPort)

	recvConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: ptt.McastPort})
	if err != nil {
		_ = sendConn.Close()

		return nil, nil, "", fmt.Errorf("listen UDP receiver: %w", err)
	}

	if err := recvConn.SetReadBuffer(65535); err != nil {
		_ = sendConn.Close()
		_ = recvConn.Close()

		return nil, nil, "", fmt.Errorf("set read buffer: %w", err)
	}

	if err := ptt.joinMulticastGroup(ifi, recvConn, net.ParseIP(ptt.McastAddr)); err != nil {
		_ = sendConn.Close()
		_ = recvConn.Close()

		return nil, nil, "", err
	}

	ptt.Log.Debug().Msgf("Joined multicast group %s:%d", ptt.McastAddr, ptt.McastPort)

	return sendConn, recvConn, localIP, nil
}

// ─── Stage 4: audio ───────────────────────────────────────────────────────────

// buildAudio resolves audio devices, opens the PortAudio playback stream, and
// opens (but does not start) the broadcast (capture) stream.
// rt must already have encoder, playbackBuffer, and beep buffers populated.
// Returns playback AudioStream, broadcast AudioStream, resolved input DeviceInfo, or an error.
func (ptt *PTTConfig) buildAudio(rt *PTTRuntime) (playback AudioStream, broadcast AudioStream, inDev *portaudio.DeviceInfo, err error) {
	outDev, err := ptt.resolveAudioDevice(ptt.OutputDevice, false)
	if err != nil {
		return nil, nil, nil, err
	}

	inDev, err = ptt.resolveAudioDevice(ptt.InputDevice, true)
	if err != nil {
		return nil, nil, nil, err
	}

	ptt.Log.Info().Msgf("Using audio devices: input=%s output=%s", inDev.Name, outDev.Name)

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
		default:
			for i := range out {
				out[i] = 0
			}
		}
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open playback stream: %w", err)
	}

	broadcast, err = ptt.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		_ = rawPlayback.Close()

		return nil, nil, nil, err
	}

	return &portaudioStream{rawPlayback}, broadcast, inDev, nil
}

// openBroadcastStreamOn creates a PortAudio input stream for inDev that uses
// rt.encoder to compress PCM and rt.sender to transmit each frame.
func (ptt *PTTConfig) openBroadcastStreamOn(inDev *portaudio.DeviceInfo, rt *PTTRuntime) (AudioStream, error) {
	inParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inDev,
			Channels: channels,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	stream, err := portaudio.OpenStream(inParams, func(in []float32) {
		pcm := make([]int16, len(in))
		for i, v := range in {
			pcm[i] = int16(v * 32767)
		}

		buf := make([]byte, 4000)

		n, encErr := rt.encoder.Encode(pcm, buf)
		if encErr != nil {
			return
		}

		packet := buf[:n]
		if ptt.Protocol == protocolRTP {
			packet = ptt.wrapRTP(packet, rt)
		}

		_, _ = rt.sender.Write(packet)

		if ptt.Trace {
			ptt.Log.Trace().Int("bytes", len(packet)).Msg("PTT multicast packet sent")
		}
	})
	if err != nil {
		return nil, fmt.Errorf("open broadcast stream: %w", err)
	}

	return &portaudioStream{stream}, nil
}

// reopenBroadcastStream closes the current broadcast stream and opens a new
// one using inDev.  rt.broadcastStream is updated atomically under recordMutex.
func (ptt *PTTConfig) reopenBroadcastStream(rt *PTTRuntime, inDev *portaudio.DeviceInfo) error {
	if inDev == nil {
		return errors.New("input device is not set")
	}

	if rt.broadcastStream != nil {
		_ = rt.broadcastStream.Close()
		rt.broadcastStream = nil
	}

	stream, err := ptt.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		return err
	}

	rt.broadcastStream = stream

	return nil
}

// ─── Stage 5: event source ────────────────────────────────────────────────────

// buildEventSource constructs the EventSource selected by PTTConfig.ControlSource.
func (ptt *PTTConfig) buildEventSource() (EventSource, error) {
	switch ptt.ControlSource {
	case "cm108":
		ptt.Log.Info().Msgf("🎙️ Listening for PTT on CM108 HID dongle (VID=0x%04X PID=0x%04X)",
			cm108VendorID, cm108ProductID)

		return NewCM108Source(ptt.Log), nil

	default: // "evdev" and everything else
		dev := ptt.findPTTDevice()
		if dev == nil {
			return nil, errors.New("PTT device not found")
		}

		ptt.Log.Info().Msgf("🎙️ Listening for PTT on: %s", dev.Name)

		return NewEvdevSource(dev, ptt.PTTKey, ptt.Log), nil
	}
}

// ─── Run (main event loop) ────────────────────────────────────────────────────

// Run is the main PTT event loop.  It starts the receive goroutine and the
// event source and blocks until ctx is canceled.
func (ptt *PTTConfig) Run(ctx context.Context, rt *PTTRuntime, src EventSource) {
	go ptt.receiveLoop(ctx, rt)

	events := src.Events(ctx)

	for {
		select {
		case <-ctx.Done():
			ptt.Log.Info().Msg("PTT context canceled; exiting run loop")

			return
		case ev, ok := <-events:
			if !ok {
				return
			}

			switch ev {
			case PTTDown:
				ptt.beginTransmission(rt)
			case PTTUp:
				ptt.endTransmission(rt)
			case PTTToggle:
				if ptt.isBroadcasting(rt) {
					ptt.Log.Debug().Msg("PTT toggle: stopping transmission")
					ptt.endTransmission(rt)
				} else {
					ptt.Log.Debug().Msg("PTT toggle: starting transmission")
					ptt.beginTransmission(rt)
				}
			}
		}
	}
}

// ─── Start (public entry point) ───────────────────────────────────────────────

// Start initializes all PTT subsystems and blocks until an OS interrupt is
// received.  Returns immediately if Enable is false.
func (ptt *PTTConfig) Start() {
	if !ptt.Enable {
		ptt.Log.Info().Msg("PTT functionality disabled; not starting.")

		return
	}

	ptt.applyDefaults()

	if ptt.Debug {
		ptt.logInputDeviceList()
	}

	ptt.Log.Info().Msgf(
		"Starting PTT on iface=%s mcast=%s:%d protocol=%s key=%s debug=%t trace=%t loopback=%t ptt_device=%s control_source=%s audio_hint=%s",
		ptt.Iface, ptt.McastAddr, ptt.McastPort, ptt.Protocol, ptt.PTTKey,
		ptt.Debug, ptt.Trace, ptt.Loopback, ptt.PTTDeviceName, ptt.ControlSource, ptt.AudioDeviceHint,
	)

	// ── codec ──────────────────────────────────────────────────────────────
	enc, dec, err := ptt.buildCodec()
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to build Opus codec")
	}

	// ── playback buffer + beep tones ───────────────────────────────────────
	playbackDepth := 2
	if ptt.PlaybackDepth > 0 {
		playbackDepth = ptt.PlaybackDepth
	}

	playbackBuf := make(chan []float32, playbackDepth)

	beepStart := make([]float32, frameSize)
	beepStop := make([]float32, frameSize)

	for i := range beepStart {
		beepStart[i] = float32(math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate))) * 0.2
		beepStop[i] = float32(math.Sin(2*math.Pi*600*float64(i)/float64(sampleRate))) * 0.2
	}

	// ── network ────────────────────────────────────────────────────────────
	rawSender, rawReceiver, localIP, err := ptt.buildNetwork()
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set up network")
	}

	// ── assemble runtime ───────────────────────────────────────────────────
	rt := &PTTRuntime{
		encoder:         enc,
		decoder:         dec,
		sender:          newSwappableSender(rawSender),
		receiver:        newSwappableReceiver(rawReceiver),
		playbackBuffer:  playbackBuf,
		beepBufferStart: beepStart,
		beepBufferStop:  beepStop,
	}

	rt.localIP.Store(localIP)
	defer rt.receiver.Close()

	if ptt.Protocol == protocolRTP {
		rtpID := ptt.RtpID
		if rtpID == "" {
			rtpID = localIP

			ptt.Log.Warn().Msg("RTP enabled but ptt.rtpId/hostname not set; using local IP to derive SSRC")
		}

		rt.rtpSSRC = rtpSSRCFromID(rtpID)
		rt.rtpSeq = randomRTPSeq()
	}

	ptt.runtime = rt
	activeConfig.Store(ptt)

	// ── PortAudio ──────────────────────────────────────────────────────────
	err = portaudio.Initialize()
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to initialize PortAudio")
	}

	go func() {
		<-ptt.Interrupt
		ptt.Log.Info().Msg("Received shutdown signal, cleaning up PortAudio")

		_ = portaudio.Terminate()

		os.Exit(0)
	}()

	playbackStream, broadcastStream, inDev, err := ptt.buildAudio(rt)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to build audio streams")
	}

	rt.broadcastStream = broadcastStream
	// Wire the reopen closure so comms.go has no portaudio dependency.
	rt.reopenBroadcast = func() error { return ptt.reopenBroadcastStream(rt, inDev) }

	err = playbackStream.Start()
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to start playback stream")
	}

	defer func() { _ = playbackStream.Stop() }()
	defer playbackStream.Close() //nolint:errcheck
	defer broadcastStream.Close()

	// ── PTT event source ───────────────────────────────────────────────────
	src, err := ptt.buildEventSource()
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to build PTT event source")
	}

	// ── run loop ───────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		ptt.Log.Info().Msg("Exiting PTT service")
		cancel()
	}()

	ptt.Run(ctx, rt, src)
}

// ─── Runtime network reconfiguration ─────────────────────────────────────────

// replaceNetwork atomically swaps the sender, receiver, and local IP stored in
// a running PTTRuntime, then closes the old sockets.  Closing the old receiver
// unblocks any in-flight ReadFromUDP in receiveLoop, which will immediately
// loop back and read from the new receiver.
//
// This is an internal helper called by UpdateMulticastEndpoint.  It may also
// be called directly in tests to exercise the swap path without real sockets.
func (ptt *PTTConfig) replaceNetwork(rt *PTTRuntime, newSender PacketWriter, newReceiver PacketReader, newLocalIP string) {
	// Swap receiver first so that receiveLoop is already pointing at the new
	// socket when the old socket's Close() unblocks its current ReadFromUDP.
	oldReceiver := rt.receiver.swap(newReceiver)
	oldSender := rt.sender.swap(newSender)
	rt.localIP.Store(newLocalIP)

	// Close old sockets after the swap.
	_ = oldReceiver.Close()
	if c, ok := oldSender.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

// UpdateMulticastEndpoint changes the multicast group address and UDP port
// used by the live PTT subsystem.  It is safe to call concurrently with the
// send/receive path and from anywhere in the application.
//
// A new pair of UDP sockets is established for (addr, port) before the swap
// so the subsystem is never left without functional sockets if the new
// endpoint fails to bind.  On error the old sockets are kept unchanged.
//
// Errors are returned for:
//   - the PTT subsystem not yet running (Start not called)
//   - addr not being a valid IPv4 multicast address
//   - port outside [1, 65535]
//   - failure to establish new UDP sockets
func UpdateMulticastEndpoint(addr string, port int) error {
	ptt := activeConfig.Load()
	if ptt == nil || ptt.runtime == nil {
		return errors.New("ptt: subsystem is not running")
	}

	rt := ptt.runtime

	ip := net.ParseIP(addr)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("ptt: %q is not a valid IPv4 address", addr)
	}

	if !ip.IsMulticast() {
		return fmt.Errorf("ptt: %q is not a multicast address", addr)
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("ptt: port %d is out of range [1, 65535]", port)
	}

	// Temporarily update config so buildNetwork picks up the new values.
	oldAddr, oldPort := ptt.McastAddr, ptt.McastPort
	ptt.McastAddr, ptt.McastPort = addr, port

	newSender, newReceiver, newLocalIP, err := ptt.buildNetwork()
	if err != nil {
		ptt.McastAddr, ptt.McastPort = oldAddr, oldPort // roll back config

		return fmt.Errorf("ptt: failed to establish %s:%d: %w", addr, port, err)
	}

	ptt.replaceNetwork(rt, newSender, newReceiver, newLocalIP)

	ptt.Log.Info().Msgf("PTT multicast endpoint updated: %s:%d → %s:%d",
		oldAddr, oldPort, addr, port)

	return nil
}
