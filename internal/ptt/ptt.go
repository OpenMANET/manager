package ptt

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/gordonklaus/portaudio"
	"github.com/hraban/opus"
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

type Config struct {
	Enable    bool
	Iface     string
	McastAddr string
	McastPort int
	PttKey    string
	Debug     bool
	Loopback  bool
	PttDevice string
}

func Start(cfg Config) {
	if !cfg.Enable {
		log.Println("PTT functionality disabled; not starting.")
		return
	}

	// apply config
	if cfg.Iface != "" {
		ifaceName = cfg.Iface
	}
	if cfg.McastAddr != "" {
		mcastAddr = cfg.McastAddr
	}
	if cfg.McastPort != 0 {
		mcastPort = cfg.McastPort
	}
	if cfg.PttKey != "" {
		pttKey = cfg.PttKey
	}

	debugEnabled = cfg.Debug
	loopbackAudio = cfg.Loopback

	if cfg.PttDevice != "" {
		pttDeviceName = cfg.PttDevice
	}

	debugf("Config: iface=%s mcast=%s:%d key=%s debug=%t loopback=%t ptt_device=%s", ifaceName, mcastAddr, mcastPort, pttKey, debugEnabled, loopbackAudio, pttDeviceName)

	var err error
	encoder, err = opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		log.Fatalf("opus.NewEncoder: %v", err)
	}

	if err := encoder.SetBitrate(targetBitrate); err != nil {
		log.Fatalf("encoder.SetBitrate: %v", err)
	}

	if err := encoder.SetComplexity(encoderComplexity); err != nil {
		log.Fatalf("encoder.SetComplexity: %v", err)
	}

	if err := encoder.SetInBandFEC(true); err != nil {
		log.Fatalf("encoder.SetInBandFEC: %v", err)
	}

	if err := encoder.SetPacketLossPerc(packetLossPerc); err != nil {
		log.Fatalf("encoder.SetPacketLossPerc: %v", err)
	}

	if err := encoder.SetDTX(false); err != nil {
		log.Fatalf("encoder.SetDTX: %v", err)
	}

	decoder, err = opus.NewDecoder(sampleRate, channels)
	if err != nil {
		log.Fatalf("opus.NewDecoder: %v", err)
	}

	if err := portaudio.Initialize(); err != nil {
		log.Fatalf("portaudio.Initialize: %v", err)
	}
	defer portaudio.Terminate()

	// playback stream
	device := getDeviceByIndex(1)
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
			debugf("Playback callback filled %d samples", len(data))
		default:
			for i := range out {
				out[i] = 0
			}
		}
	})
	if err != nil {
		log.Fatalf("portaudio.OpenStream: %v", err)
	}

	if err := playbackStream.Start(); err != nil {
		log.Fatalf("playbackStream.Start: %v", err)
	}
	defer playbackStream.Stop()
	defer playbackStream.Close()

	// mic stream (opened, not started)
	broadcastStream, err = portaudio.OpenDefaultStream(channels, 0, float64(sampleRate), frameSize, func(in []float32) {
		debugf("Mic callback received %d samples", len(in))
		pcm := make([]int16, len(in))

		for i, v := range in {
			pcm[i] = int16(v * 32767)
		}

		buf := make([]byte, 4000)
		if n, err := encoder.Encode(pcm, buf); err == nil {
			_, _ = udpSendConn.Write(buf[:n])
			debugf("Encoded %d bytes from mic callback", n)
		}
	})
	if err != nil {
		log.Fatalf("portaudio.OpenDefaultStream: %v", err)
	}

	defer broadcastStream.Close()

	// beeps
	for i := range beepBufferStart {
		beepBufferStart[i] = float32(math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate))) * 0.2
		beepBufferStop[i] = float32(math.Sin(2*math.Pi*600*float64(i)/float64(sampleRate))) * 0.2
	}

	// networking: bind send to iface IP; listen on :port and join group on iface
	ifIP, ifi, err := getIfaceIPv4(ifaceName)
	if err != nil {
		log.Fatalf("getIfaceIPv4: %v", err)
	}

	localIP = ifIP
	debugf("Using interface %s with IP %s", ifaceName, ifIP)

	// sender bound to iface IP so traffic egresses that iface
	dst := &net.UDPAddr{IP: net.ParseIP(mcastAddr), Port: mcastPort}
	src := &net.UDPAddr{IP: net.ParseIP(ifIP), Port: 0}

	udpSendConn, err = net.DialUDP("udp4", src, dst)
	if err != nil {
		log.Fatalf("net.DialUDP: %v", err)
	}
	debugf("Sender bound to %s -> %s:%d", src.IP.String(), mcastAddr, mcastPort)

	// receiver on all, then join group on iface
	udpRecvConn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: mcastPort})
	if err != nil {
		log.Fatalf("net.ListenUDP: %v", err)
	}

	if err := udpRecvConn.SetReadBuffer(65535); err != nil {
		log.Fatalf("udpRecvConn.SetReadBuffer: %v", err)
	}

	if err := joinMulticastGroup(ifi, udpRecvConn, net.ParseIP(mcastAddr)); err != nil {
		log.Fatalf("joinMulticastGroup: %v", err)
	}
	debugf("Joined multicast group %s:%d", mcastAddr, mcastPort)

	go receiveLoop(udpRecvConn)

	// PTT input (kept as-is for now)
	ptt := findPTTDevice()
	fmt.Println("🎙️ Listening for PTT on:", ptt.Name)
	debugf("Monitoring PTT device %s", ptt.Name)
	go monitorPTT(ptt, broadcastStream)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	fmt.Println("Exiting.")
}

/********* app *********/
func main() {
	listFlag := flag.Bool("l", false, "List input devices and exit")
	flag.Parse()
	if *listFlag {
		logInputDeviceList()
		return
	}

}

func debugf(format string, args ...interface{}) {
	if !debugEnabled {
		return
	}
	log.Printf("[DEBUG] "+format, args...)
}
