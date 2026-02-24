package ptt

import (
	"errors"
	"math"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/gordonklaus/portaudio"
	"github.com/hraban/opus"
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
	defaultIface         string = "br-ahwlan" // ← use bridge by default; override in UCI if needed
	defaultG             string = "224.0.0.1"
	defaultPort          int    = 5007
	defaultProtocol      string = protocolUDP
	defaultDebug         bool   = true
	defaultLoopback      bool   = true
	defaultPTTDevice     string = "/dev/hidraw0/*"
	defaultPTTDeviceName string = "AllInOneCable"
	defaultControlSource string = "evdev"
)

// PTTRuntime holds all resources and derived state for a running PTT instance.
// It is allocated by Start once Enable is confirmed and is nil otherwise.
// No field is safe to access concurrently unless documented below.
type PTTRuntime struct {
	// ---- Codec ----

	// encoder is the Opus encoder used in the broadcast-stream PortAudio
	// callback to convert float32 PCM captured from the microphone into a
	// compressed Opus byte slice before it is sent over UDP.
	encoder *opus.Encoder

	// decoder is the Opus decoder used in decodeAndQueue and
	// decodeAndQueuePLC to turn received Opus payloads (or a nil PLC
	// stimulus) back into float32 PCM for the playback buffer.
	decoder *opus.Decoder

	// ---- Network ----

	// udpSendConn is a connected UDP socket bound to the local interface IP
	// so that outbound multicast datagrams egress the correct interface.
	// Written to inside the PortAudio capture callback.
	udpSendConn *net.UDPConn

	// udpRecvConn is a UDP socket bound to 0.0.0.0:<mcastPort> that has
	// joined the multicast group on the selected interface.  Owned by
	// receiveLoop.
	udpRecvConn *net.UDPConn

	// localIP is the IPv4 address of the selected network interface.
	// Used in receiveLoop to identify and optionally drop packets that
	// originated from this node.
	localIP string

	// protocol is the normalized wire-framing mode ("udp" or "rtp").
	// Set once at startup from PTTConfig.Protocol; read in receiveLoop and
	// in the broadcast-stream callback to decide whether to prepend an RTP
	// header.
	protocol string

	// rtpSeq is the rolling RTP sequence number written into every outbound
	// RTP header.  Incremented by wrapRTP on each sent packet.
	rtpSeq uint16

	// rtpSSRC is the synchronisation source identifier written into every
	// outbound RTP header.  Derived once at startup as the FNV-1a hash of
	// rtpID.
	rtpSSRC uint32

	// rtpID is the string from which rtpSSRC is hashed.  Resolved in
	// priority order: PTTConfig.RtpID → system hostname → local interface
	// IP.
	rtpID string

	// ---- Audio ----

	// broadcastStream is the PortAudio input stream capturing microphone
	// audio.  Its callback encodes and sends each frame over UDP.
	// Started/stopped by beginTransmission and endTransmission; re-opened
	// by reopenBroadcastStream if a Start failure is detected.
	broadcastStream *portaudio.Stream

	// inputDevice is the resolved PortAudio DeviceInfo for the capture
	// device.  Stored so that reopenBroadcastStream can re-open the stream
	// without re-running the full device resolution logic.
	inputDevice *portaudio.DeviceInfo

	// playbackBuffer is a buffered channel of decoded float32 PCM frames.
	// The PortAudio output callback drains it on each 20 ms tick; the
	// receive path and PLC path write into it via decodeAndQueue /
	// decodeAndQueuePLC.
	playbackBuffer chan []float32

	// beepBufferStart is a single 20 ms frame of a 1000 Hz sine wave at
	// 20 % amplitude.  It is played to the local speaker immediately when
	// PTT is pressed to provide audible transmit-start feedback.
	beepBufferStart []float32

	// beepBufferStop is a single 20 ms frame of a 600 Hz sine wave at
	// 20 % amplitude.  It is played to the local speaker when PTT is
	// released to signal end of transmission.
	beepBufferStop []float32

	// ---- Resolved config (applied from PTTConfig with fallbacks) ----

	// ifaceName is the network interface name after applying the default
	// (br-ahwlan).  Used to look up the interface's IPv4 address and to
	// join the multicast group.
	ifaceName string

	// mcastAddr is the multicast group address after applying the default
	// (224.0.0.1).  Used as the UDP send destination and for the multicast
	// group join on the receive socket.
	mcastAddr string

	// mcastPort is the UDP port after applying the default (5007).  Used
	// as both the send destination port and the receive bind port.
	mcastPort int

	// pttKey is the evdev key filter after applying the default ("any").
	// "any" matches all key press events; a decimal integer matches the
	// specific Linux EV_KEY code.  Read in monitorPTT on every key event.
	pttKey string

	// pttDevice is the file-system glob after applying the default
	// (/dev/hidraw0/*).  Passed to evdev.ListInputDevices to narrow the
	// set of devices scanned when looking for the PTT button.
	pttDevice string

	// pttDeviceName is the exact evdev device name after applying the
	// default ("AllInOneCable").  Compared against each enumerated device's
	// Name in findPTTDevice to select the PTT button hardware.
	pttDeviceName string

	// controlSource is the normalised PTT event backend after applying the
	// default ("evdev").  Read in Start to choose between the evdev loop
	// and the bluealsa_xevent journal monitor.
	controlSource string

	// audioDeviceHint is the shared substring matcher applied to both
	// InputDevice and OutputDevice when neither is set explicitly.
	// Empty string disables hint-based matching.
	audioDeviceHint string

	// ---- Transmission state ----

	// recordMutex guards the broadcasting field.  Must be held whenever
	// broadcasting is read or written outside of the same goroutine.
	recordMutex sync.Mutex

	// broadcasting is true while the broadcast stream is actively capturing
	// and sending microphone audio.  Guarded by recordMutex; toggled by
	// beginTransmission and endTransmission.
	broadcasting bool

	// ---- Flags ----

	// debugEnabled mirrors PTTConfig.Debug.  When true, device enumeration
	// results and other verbose startup details are logged at Debug level.
	debugEnabled bool

	// loopbackAudio mirrors PTTConfig.Loopback.  When false, receiveLoop
	// silently drops datagrams whose source IP is the loopback address or
	// the local interface IP, preventing self-monitoring.
	loopbackAudio bool

	// traceEnabled mirrors PTTConfig.Trace.  When true, every inbound and
	// outbound datagram is logged at Trace level with source address, byte
	// count, and RTP header fields when present.
	traceEnabled bool
}

// PTTConfig holds the static configuration for the PTT subsystem.
// Allocate one with NewPTT and call Start to begin operation.
// All fields must be set before Start is called; none are mutated afterwards.
type PTTConfig struct {
	// Log is the zerolog logger used for all PTT subsystem messages.
	Log zerolog.Logger

	// Interrupt is the OS signal channel used to trigger a clean shutdown of
	// the PTT service.  Typically populated from os/signal.Notify.
	Interrupt chan os.Signal

	// Enable controls whether the PTT subsystem starts at all.
	// When false, Start returns immediately without allocating any resources.
	Enable bool

	// ---- Network ----

	// Iface is the name of the network interface whose IPv4 address is used to
	// bind the outbound UDP multicast sender and join the multicast group on
	// the receiver side.
	Iface string

	// McastAddr is the IPv4 multicast group address (e.g. "224.0.0.1").
	McastAddr string

	// McastPort is the UDP port used for both sending and receiving multicast
	// audio datagrams.
	McastPort int

	// Protocol selects the wire framing for audio datagrams.  "udp" sends raw
	// Opus payloads; "rtp" prefixes each payload with a 12-byte RTP header.
	// Receive path auto-detects RTP regardless of this setting.
	Protocol string

	// RtpID is the identifier used to derive the RTP SSRC via FNV-1a hashing.
	// Defaults to the system hostname, falling back to the local interface IP.
	// Set to the ATAK device identifier for strict VX multicast compatibility.
	RtpID string

	// ---- Audio ----

	// InputDevice specifies the PortAudio capture device.  Accepts a device
	// index (as a decimal string), an exact device name, or a name substring.
	// Empty string selects the system default input device.
	InputDevice string

	// OutputDevice specifies the PortAudio playback device.  Accepts a device
	// index (as a decimal string), an exact device name, or a name substring.
	// Empty string selects the system default output device.
	OutputDevice string

	// AudioDeviceHint is a shared substring matcher applied to both
	// InputDevice and OutputDevice when neither is explicitly set.  Useful
	// when a single keyword (e.g. "BS-22") uniquely identifies both sides of
	// a Bluetooth speaker-mic.
	AudioDeviceHint string

	// PlaybackDepth controls the depth of the decoded-audio playback channel.
	// Larger values tolerate more network jitter at the cost of added latency.
	// Defaults to 2 when unset or zero.
	PlaybackDepth int

	// ---- PTT control ----

	// PTTKey is the evdev key code that activates the transmit path.
	// Use "any" to treat every key event as a PTT trigger, or provide a
	// decimal integer matching a Linux EV_KEY code.
	PTTKey string

	// PTTDeviceGlob is the file-system glob used to enumerate evdev input
	// devices when searching for the PTT button hardware
	// (e.g. "/dev/hidraw0/*").
	PTTDeviceGlob string

	// PTTDeviceName is the exact device name as reported by evdev that will
	// be opened as the PTT button source.
	PTTDeviceName string

	// ControlSource selects the PTT event backend.
	// "evdev" (default) reads Linux input events via the evdev API.
	// "bluealsa_xevent" tails the BlueALSA journal for AT+XEVENT=PTT_DOWN /
	// PTT_UP vendor events from paired Bluetooth headsets.
	ControlSource string

	// ---- Flags ----

	// Debug enables device-enumeration and startup debug logging.
	Debug bool

	// Loopback controls whether audio transmitted by this node is also played
	// back locally.  When false, datagrams sourced from the local interface
	// IP are silently dropped on the receive path.
	Loopback bool

	// Trace enables per-packet trace logging on both the send and receive
	// paths, including RTP header fields when present.
	Trace bool

	// runtime holds internally-allocated resources and is populated by Start.
	// It is nil when Enable is false or before Start is called.
	runtime *PTTRuntime
}

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

func (ptt *PTTConfig) Start() {
	if !ptt.Enable {
		ptt.Log.Info().Msg("PTT functionality disabled; not starting.")
		return
	}

	// Initialize runtime state only when PTT is enabled
	ptt.runtime = &PTTRuntime{
		ifaceName:     defaultIface,
		mcastAddr:     defaultG,
		mcastPort:     defaultPort,
		protocol:      defaultProtocol,
		rtpID:         "",
		pttKey:        defaultKey,
		debugEnabled:  defaultDebug,
		loopbackAudio: defaultLoopback,
		pttDeviceName: defaultPTTDeviceName,
		pttDevice:     defaultPTTDevice,
		controlSource: defaultControlSource,
	}

	// apply config
	if ptt.Iface != "" {
		ptt.runtime.ifaceName = ptt.Iface
	}
	if ptt.McastAddr != "" {
		ptt.runtime.mcastAddr = ptt.McastAddr
	}
	if ptt.McastPort != 0 {
		ptt.runtime.mcastPort = ptt.McastPort
	}
	if ptt.PTTKey != "" {
		ptt.runtime.pttKey = ptt.PTTKey
	} else {
		ptt.runtime.pttKey = defaultKey
	}

	if ptt.Protocol != "" {
		ptt.runtime.protocol = normalizeProtocol(ptt.Protocol)
	} else {
		ptt.runtime.protocol = defaultProtocol
	}

	if ptt.RtpID != "" {
		ptt.runtime.rtpID = ptt.RtpID
	} else if hostname, err := os.Hostname(); err == nil && hostname != "" {
		ptt.runtime.rtpID = hostname
		ptt.Log.Debug().Msgf("RTP ID not set; using hostname %q", hostname)
	}

	ptt.runtime.debugEnabled = ptt.Debug
	if ptt.runtime.debugEnabled {
		ptt.logInputDeviceList()
	}

	ptt.runtime.loopbackAudio = ptt.Loopback
	ptt.runtime.traceEnabled = ptt.Trace

	if ptt.PTTDeviceGlob != "" {
		ptt.runtime.pttDevice = ptt.PTTDeviceGlob
	}

	if ptt.PTTDeviceName != "" {
		ptt.runtime.pttDeviceName = ptt.PTTDeviceName
	}

	if ptt.ControlSource != "" {
		ptt.runtime.controlSource = ptt.ControlSource
	}
	if ptt.AudioDeviceHint != "" {
		ptt.runtime.audioDeviceHint = ptt.AudioDeviceHint
	}

	playbackDepth := 2
	if ptt.PlaybackDepth > 0 {
		playbackDepth = ptt.PlaybackDepth
	}
	ptt.runtime.playbackBuffer = make(chan []float32, playbackDepth)
	ptt.runtime.beepBufferStart = make([]float32, frameSize)
	ptt.runtime.beepBufferStop = make([]float32, frameSize)

	ptt.Log.Info().Msgf("Starting PTT on iface=%s mcast=%s:%d protocol=%s key=%s debug=%t trace=%t loopback=%t ptt_device=%s control_source=%s audio_hint=%s", ptt.runtime.ifaceName, ptt.runtime.mcastAddr, ptt.runtime.mcastPort, ptt.runtime.protocol, ptt.runtime.pttKey, ptt.runtime.debugEnabled, ptt.runtime.traceEnabled, ptt.runtime.loopbackAudio, ptt.runtime.pttDeviceName, ptt.runtime.controlSource, ptt.runtime.audioDeviceHint)

	var err error
	ptt.runtime.encoder, err = opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to create Opus encoder")
	}

	if err := ptt.runtime.encoder.SetBitrate(targetBitrate); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder bitrate")
	}

	if err := ptt.runtime.encoder.SetComplexity(encoderComplexity); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder complexity")
	}

	if err := ptt.runtime.encoder.SetInBandFEC(false); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder in-band FEC")
	}

	if err := ptt.runtime.encoder.SetPacketLossPerc(packetLossPerc); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder packet loss percentage")
	}

	if err := ptt.runtime.encoder.SetDTX(false); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder DTX")
	}

	ptt.runtime.decoder, err = opus.NewDecoder(sampleRate, channels)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to create Opus decoder")
	}

	if err := portaudio.Initialize(); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to initialize PortAudio")
	}

	inputSpec, outputSpec := ptt.resolveAudioSpecs()

	outDev, err := ptt.resolveAudioDevice(outputSpec, false)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to resolve output audio device")
	}

	inDev, err := ptt.resolveAudioDevice(inputSpec, true)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to resolve input audio device")
	}

	ptt.Log.Info().Msgf("Using audio devices: input=%s output=%s", inDev.Name, outDev.Name)

	// handle shutdown
	go func() {
		<-ptt.Interrupt
		ptt.Log.Info().Msg("Received shutdown signal, cleaning up PortAudio")
		portaudio.Terminate()
		os.Exit(0)
	}()

	// playback stream
	params := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outDev,
			Channels: channels,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	playbackStream, err := portaudio.OpenStream(params, func(_, out []float32) {
		select {
		case data := <-ptt.runtime.playbackBuffer:
			copy(out, data)
		default:
			for i := range out {
				out[i] = 0
			}
		}
	})
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to open PortAudio stream")
	}

	if err := playbackStream.Start(); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to start playback stream")
	}
	defer playbackStream.Stop()
	defer playbackStream.Close()

	ptt.runtime.inputDevice = inDev
	if err := ptt.openBroadcastStream(inDev); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to open PortAudio stream")
	}

	defer ptt.runtime.broadcastStream.Close()

	// beeps
	for i := range ptt.runtime.beepBufferStart {
		ptt.runtime.beepBufferStart[i] = float32(math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate))) * 0.2
		ptt.runtime.beepBufferStop[i] = float32(math.Sin(2*math.Pi*600*float64(i)/float64(sampleRate))) * 0.2
	}

	// networking: bind send to iface IP; listen on :port and join group on iface
	ifIP, ifi, err := ptt.getIfaceIPv4(ptt.runtime.ifaceName)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to get interface IPv4")
	}

	ptt.runtime.localIP = ifIP
	if ptt.runtime.protocol == protocolRTP {
		rtpID := ptt.runtime.rtpID
		if rtpID == "" {
			rtpID = ifIP
			ptt.Log.Warn().Msg("RTP enabled but ptt.rtpId/hostname not set; using local IP to derive SSRC")
		}
		ptt.runtime.rtpSSRC = rtpSSRCFromID(rtpID)
		ptt.runtime.rtpSeq = randomRTPSeq()
	}
	ptt.Log.Debug().Msgf("Using interface %s with IP %s", ptt.runtime.ifaceName, ifIP)

	// sender bound to iface IP so traffic egresses that iface
	dst := &net.UDPAddr{IP: net.ParseIP(ptt.runtime.mcastAddr), Port: ptt.runtime.mcastPort}
	src := &net.UDPAddr{IP: net.ParseIP(ifIP), Port: 0}

	ptt.runtime.udpSendConn, err = net.DialUDP("udp4", src, dst)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to dial UDP")
	}
	ptt.Log.Debug().Msgf("Sender bound to %s -> %s:%d", src.IP.String(), ptt.runtime.mcastAddr, ptt.runtime.mcastPort)

	// receiver on all, then join group on iface
	ptt.runtime.udpRecvConn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: ptt.runtime.mcastPort})
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to listen on UDP")
	}

	if err := ptt.runtime.udpRecvConn.SetReadBuffer(65535); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set UDP read buffer")
	}

	if err := ptt.joinMulticastGroup(ifi, ptt.runtime.udpRecvConn, net.ParseIP(ptt.runtime.mcastAddr)); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to join multicast group")
	}
	ptt.Log.Debug().Msgf("Joined multicast group %s:%d", ptt.runtime.mcastAddr, ptt.runtime.mcastPort)

	go ptt.receiveLoop(ptt.runtime.udpRecvConn)

	switch normalizeControlSource(ptt.runtime.controlSource) {
	case "bluealsa_xevent":
		ptt.Log.Info().Msg("Listening for PTT using BlueALSA XEVENT backend")
		go ptt.monitorBluealsaXEvents()
	default:
		pttDevice := ptt.findPTTDevice()
		ptt.Log.Info().Msgf("🎙️ Listening for PTT on: %s", pttDevice.Name)
		ptt.Log.Debug().Msgf("Monitoring PTT device %s", pttDevice.Name)
		go ptt.monitorPTT(pttDevice)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	ptt.Log.Info().Msg("Exiting PTT service")
}

func (ptt *PTTConfig) openBroadcastStream(inDev *portaudio.DeviceInfo) error {
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
		if n, err := ptt.runtime.encoder.Encode(pcm, buf); err == nil {
			packet := buf[:n]
			if ptt.runtime.protocol == protocolRTP {
				packet = ptt.wrapRTP(packet)
			}
			_, _ = ptt.runtime.udpSendConn.Write(packet)
		}
	})
	if err != nil {
		return err
	}
	ptt.runtime.broadcastStream = stream
	return nil
}

func (ptt *PTTConfig) reopenBroadcastStream() error {
	if ptt.runtime.inputDevice == nil {
		return errors.New("input device is not set")
	}

	if ptt.runtime.broadcastStream != nil {
		_ = ptt.runtime.broadcastStream.Close()
		ptt.runtime.broadcastStream = nil
	}

	return ptt.openBroadcastStream(ptt.runtime.inputDevice)
}
