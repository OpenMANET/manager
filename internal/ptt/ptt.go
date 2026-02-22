package ptt

import (
	"errors"
	"math"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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

// PTTRuntime holds runtime state for an active PTT instance
// Only allocated when PTT is enabled to save memory
type PTTRuntime struct {
	// codec/network
	encoder         *opus.Encoder
	decoder         *opus.Decoder
	udpSendConn     *net.UDPConn
	udpRecvConn     *net.UDPConn
	playbackBuffer  chan []float32
	broadcastStream *portaudio.Stream
	localIP         string
	protocol        string
	rtpSeq          uint16
	rtpSSRC         uint32
	rtpID           string
	inputDevice     *portaudio.DeviceInfo

	// config from UCI (with fallbacks)
	ifaceName       string
	mcastAddr       string
	pttKey          string
	pttDeviceName   string
	pttDevice       string
	controlSource   string
	audioDeviceHint string
	beepBufferStart []float32
	beepBufferStop  []float32
	mcastPort       int
	recordMutex     sync.Mutex

	broadcasting  bool
	debugEnabled  bool
	loopbackAudio bool
	traceEnabled  bool
}

type PTTConfig struct {
	Log      zerolog.Logger
	Interupt chan os.Signal

	// Runtime state - only allocated when PTT is enabled
	runtime         *PTTRuntime
	Iface           string
	McastAddr       string
	PttKey          string
	PttDevice       string
	PttDeviceName   string
	ControlSource   string
	AudioDeviceHint string
	InputDevice     string
	OutputDevice    string
	PlaybackDepth   int
	Protocol        string
	RtpID           string
	McastPort       int
	Enable          bool
	Debug           bool
	Loopback        bool
	Trace           bool
}

func NewPTT(cfg PTTConfig) *PTTConfig {
	return &PTTConfig{
		Log:             cfg.Log,
		Interupt:        cfg.Interupt,
		Enable:          cfg.Enable,
		Iface:           cfg.Iface,
		McastAddr:       cfg.McastAddr,
		McastPort:       cfg.McastPort,
		PttKey:          cfg.PttKey,
		Protocol:        cfg.Protocol,
		RtpID:           cfg.RtpID,
		Debug:           cfg.Debug,
		Loopback:        cfg.Loopback,
		Trace:           cfg.Trace,
		PttDevice:       cfg.PttDevice,
		PttDeviceName:   cfg.PttDeviceName,
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

	for {
		if err := ptt.startOnce(); err != nil {
			ptt.Log.Error().Err(err).Msgf("PTT startup failed; retrying in %s", pttRetryDelay)
			time.Sleep(pttRetryDelay)
			continue
		}

		return
	}
}

func (ptt *PTTConfig) startOnce() error {
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
	if ptt.PttKey != "" {
		ptt.runtime.pttKey = ptt.PttKey
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

	if ptt.PttDevice != "" {
		ptt.runtime.pttDevice = ptt.PttDevice
	}

	if ptt.PttDeviceName != "" {
		ptt.runtime.pttDeviceName = ptt.PttDeviceName
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

	var (
		err                  error
		playbackStream       *portaudio.Stream
		portAudioInitialized bool
	)

	defer func() {
		if err == nil {
			return
		}

		if ptt.runtime.broadcastStream != nil {
			_ = ptt.runtime.broadcastStream.Close()
			ptt.runtime.broadcastStream = nil
		}

		if playbackStream != nil {
			_ = playbackStream.Stop()
			_ = playbackStream.Close()
		}

		if ptt.runtime.udpSendConn != nil {
			_ = ptt.runtime.udpSendConn.Close()
			ptt.runtime.udpSendConn = nil
		}

		if ptt.runtime.udpRecvConn != nil {
			_ = ptt.runtime.udpRecvConn.Close()
			ptt.runtime.udpRecvConn = nil
		}

		if portAudioInitialized {
			portaudio.Terminate()
		}
	}()

	ptt.runtime.encoder, err = opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return fmt.Errorf("failed to create Opus encoder: %w", err)
	}

	if err := ptt.runtime.encoder.SetBitrate(targetBitrate); err != nil {
		return fmt.Errorf("failed to set Opus encoder bitrate: %w", err)
	}

	if err := ptt.runtime.encoder.SetComplexity(encoderComplexity); err != nil {
		return fmt.Errorf("failed to set Opus encoder complexity: %w", err)
	}

	if err := ptt.runtime.encoder.SetInBandFEC(false); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder in-band FEC")
	}

	if err := ptt.runtime.encoder.SetPacketLossPerc(packetLossPerc); err != nil {
		return fmt.Errorf("failed to set Opus encoder packet loss percentage: %w", err)
	}

	if err := ptt.runtime.encoder.SetDTX(false); err != nil {
		return fmt.Errorf("failed to set Opus encoder DTX: %w", err)
	}

	ptt.runtime.decoder, err = opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return fmt.Errorf("failed to create Opus decoder: %w", err)
	}

	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize PortAudio: %w", err)
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
		<-ptt.Interupt
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

	playbackStream, err = portaudio.OpenStream(params, func(_, out []float32) {
		select {
		case data := <-ptt.runtime.playbackBuffer:
			copy(out, data)
			ptt.Log.Debug().Msgf("Playback callback filled %d samples", len(data))
		default:
			for i := range out {
				out[i] = 0
			}
		}
	})
	if err != nil {
		return fmt.Errorf("failed to open PortAudio playback stream: %w", err)
	}

	if err := playbackStream.Start(); err != nil {
		return fmt.Errorf("failed to start PortAudio playback stream: %w", err)
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
		return fmt.Errorf("failed to get interface IPv4: %w", err)
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
		return fmt.Errorf("failed to dial UDP: %w", err)
	}
	ptt.Log.Debug().Msgf("Sender bound to %s -> %s:%d", src.IP.String(), ptt.runtime.mcastAddr, ptt.runtime.mcastPort)

	// receiver on all, then join group on iface
	ptt.runtime.udpRecvConn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: ptt.runtime.mcastPort})
	if err != nil {
		return fmt.Errorf("failed to listen on UDP: %w", err)
	}

	if err := ptt.runtime.udpRecvConn.SetReadBuffer(65535); err != nil {
		return fmt.Errorf("failed to set UDP read buffer: %w", err)
	}

	if err := ptt.joinMulticastGroup(ifi, ptt.runtime.udpRecvConn, net.ParseIP(ptt.runtime.mcastAddr)); err != nil {
		return fmt.Errorf("failed to join multicast group: %w", err)
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

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdown)
	<-shutdown
	ptt.Log.Info().Msg("Exiting PTT service")
	portaudio.Terminate()

	return nil
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
		ptt.Log.Debug().Msgf("Mic callback received %d samples", len(in))
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
			ptt.Log.Debug().Msgf("Encoded %d bytes from mic callback", n)
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
