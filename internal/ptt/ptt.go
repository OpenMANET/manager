package ptt

import (
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
	sampleRate        int    = 48000
	channels          int    = 1
	frameSize         int    = 960
	targetBitrate     int    = 12000
	encoderComplexity int    = 3
	packetLossPerc    int    = 10
	defaultKey        string = "any"
	defaultIface      string = "br-ahwlan" // ← use bridge by default; override in UCI if needed
	defaultG          string = "224.0.0.1"
	defaultPort       int    = 5007
	defaultDebug      bool   = true
	defaultLoopback   bool   = true
	defaultPTTDevice  string = "Generic AB13X USB Audio"
)

var (
	// codec/network
	encoder         *opus.Encoder
	decoder         *opus.Decoder
	udpSendConn     *net.UDPConn
	udpRecvConn     *net.UDPConn
	localIP         string
	playbackBuffer  = make(chan []float32, 2)
	beepBufferStart = make([]float32, frameSize)
	beepBufferStop  = make([]float32, frameSize)
	broadcastStream *portaudio.Stream
	broadcasting    bool
	recordMutex     sync.Mutex

	// config from UCI (with fallbacks)
	ifaceName     = defaultIface
	mcastAddr     = defaultG
	mcastPort     = defaultPort
	pttKey        = defaultKey
	debugEnabled  = defaultDebug
	loopbackAudio = defaultLoopback
	pttDeviceName = defaultPTTDevice
)

type PTTConfig struct {
	Log       zerolog.Logger
	Enable    bool
	Iface     string
	McastAddr string
	McastPort int
	PttKey    string
	Debug     bool
	Loopback  bool
	PttDevice string
}

func NewPTT(cfg PTTConfig) *PTTConfig {
	return &PTTConfig{
		Log:       cfg.Log,
		Enable:    cfg.Enable,
		Iface:     cfg.Iface,
		McastAddr: cfg.McastAddr,
		McastPort: cfg.McastPort,
		PttKey:    cfg.PttKey,
		Debug:     cfg.Debug,
		Loopback:  cfg.Loopback,
		PttDevice: cfg.PttDevice,
	}
}

func (ptt *PTTConfig) Start() {
	if !ptt.Enable {
		ptt.Log.Info().Msg("PTT functionality disabled; not starting.")
		return
	}

	// apply config
	if ptt.Iface != "" {
		ifaceName = ptt.Iface
	}
	if ptt.McastAddr != "" {
		mcastAddr = ptt.McastAddr
	}
	if ptt.McastPort != 0 {
		mcastPort = ptt.McastPort
	}
	if ptt.PttKey != "" {
		pttKey = ptt.PttKey
	}

	debugEnabled = ptt.Debug
	loopbackAudio = ptt.Loopback

	if ptt.PttDevice != "" {
		pttDeviceName = ptt.PttDevice
	}

	ptt.Log.Info().Msgf("Starting PTT on iface=%s mcast=%s:%d key=%s debug=%t loopback=%t ptt_device=%s", ifaceName, mcastAddr, mcastPort, pttKey, debugEnabled, loopbackAudio, pttDeviceName)

	var err error
	encoder, err = opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to create Opus encoder")
	}

	if err := encoder.SetBitrate(targetBitrate); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder bitrate")
	}

	if err := encoder.SetComplexity(encoderComplexity); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder complexity")
	}

	if err := encoder.SetInBandFEC(true); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder in-band FEC")
	}

	if err := encoder.SetPacketLossPerc(packetLossPerc); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder packet loss percentage")
	}

	if err := encoder.SetDTX(false); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set Opus encoder DTX")
	}

	decoder, err = opus.NewDecoder(sampleRate, channels)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to create Opus decoder")
	}

	if err := portaudio.Initialize(); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to initialize PortAudio")
	}

	// Setup signal handler for cleanup
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		ptt.Log.Info().Msg("Received shutdown signal, cleaning up PortAudio")
		portaudio.Terminate()
		os.Exit(0)
	}()
	// playback stream
	device := ptt.getDeviceByIndex(1)
	params := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   device,
			Channels: channels,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	playbackStream, err := portaudio.OpenStream(params, func(_, out []float32) {
		select {
		case data := <-playbackBuffer:
			copy(out, data)
			ptt.Log.Debug().Msgf("Playback callback filled %d samples", len(data))
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

	// mic stream (opened, not started)
	broadcastStream, err = portaudio.OpenDefaultStream(channels, 0, float64(sampleRate), frameSize, func(in []float32) {
		ptt.Log.Debug().Msgf("Mic callback received %d samples", len(in))
		pcm := make([]int16, len(in))

		for i, v := range in {
			pcm[i] = int16(v * 32767)
		}

		buf := make([]byte, 4000)
		if n, err := encoder.Encode(pcm, buf); err == nil {
			_, _ = udpSendConn.Write(buf[:n])
			ptt.Log.Debug().Msgf("Encoded %d bytes from mic callback", n)
		}
	})
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to open PortAudio stream")
	}

	defer broadcastStream.Close()

	// beeps
	for i := range beepBufferStart {
		beepBufferStart[i] = float32(math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate))) * 0.2
		beepBufferStop[i] = float32(math.Sin(2*math.Pi*600*float64(i)/float64(sampleRate))) * 0.2
	}

	// networking: bind send to iface IP; listen on :port and join group on iface
	ifIP, ifi, err := ptt.getIfaceIPv4(ifaceName)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to get interface IPv4")
	}

	localIP = ifIP
	ptt.Log.Debug().Msgf("Using interface %s with IP %s", ifaceName, ifIP)

	// sender bound to iface IP so traffic egresses that iface
	dst := &net.UDPAddr{IP: net.ParseIP(mcastAddr), Port: mcastPort}
	src := &net.UDPAddr{IP: net.ParseIP(ifIP), Port: 0}

	udpSendConn, err = net.DialUDP("udp4", src, dst)
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to dial UDP")
	}
	ptt.Log.Debug().Msgf("Sender bound to %s -> %s:%d", src.IP.String(), mcastAddr, mcastPort)

	// receiver on all, then join group on iface
	udpRecvConn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: mcastPort})
	if err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to listen on UDP")
	}

	if err := udpRecvConn.SetReadBuffer(65535); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to set UDP read buffer")
	}

	if err := ptt.joinMulticastGroup(ifi, udpRecvConn, net.ParseIP(mcastAddr)); err != nil {
		ptt.Log.Fatal().Err(err).Msg("Failed to join multicast group")
	}
	ptt.Log.Debug().Msgf("Joined multicast group %s:%d", mcastAddr, mcastPort)

	go ptt.receiveLoop(udpRecvConn)

	// PTT input (kept as-is for now)
	pttDevice := ptt.findPTTDevice()
	ptt.Log.Info().Msgf("🎙️ Listening for PTT on: %s", pttDevice.Name)
	ptt.Log.Debug().Msgf("Monitoring PTT device %s", pttDevice.Name)
	go ptt.monitorPTT(pttDevice, broadcastStream)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	ptt.Log.Info().Msg("Exiting PTT service")
}
