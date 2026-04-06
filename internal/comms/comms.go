package comms

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/openmanet/openmanetd/internal/config"
)

// ─── Package-level constants ──────────────────────────────────────────────────

const (
	sampleRate        int    = 48000
	channels          int    = 1
	frameSize         int    = 960 // 20 ms at 48 kHz
	targetBitrate     int    = 32000
	encoderComplexity int    = 10
	packetLossPerc    int    = 20
	defaultKey        string = "any"
	defaultIface      string = "br-ahwlan"
	defaultCommDevice string = "/dev/hidraw0/*"
	defaultCommName   string = "AllInOneCable"
	defaultCtrlSrc    string = "openvlm"

	// encBufSize is the maximum Opus encode output buffer. 1450 bytes matches
	// the UDP MTU and is far larger than typical Opus output (~80–160 B at
	// 32 kbps).
	encBufSize = 1450

	// rtpMulticastTTL is the IP TTL set on outgoing RTP/RTCP multicast packets.
	// A value of 1 restricts packets to the local subnet; increase to allow
	// traversal across routed multicast hops.
	rtpMulticastTTL = 1
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
//
// InitSendEnabled and InitReceiveEnabled seed the runtime atomic flags that
// EnableTalkGroupSend / EnableTalkGroupReceive toggle at runtime. When nil
// the values fall back to Send and Receive respectively, preserving backward
// compatibility for any caller that constructs McastPortConfig directly.
type McastPortConfig struct {
	InitSendEnabled    *bool
	InitReceiveEnabled *bool
	Address            string
	Port               int
	Send               bool
	Receive            bool
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
//
// jitter is the per-port RTP jitter buffer. It is allocated in
// buildSinglePortChannel for ports with a Receive socket and shared between
// receiveLoop (producer) and the PortAudio output callback (consumer). For
// portChannels constructed directly in tests, callers must allocate it
// explicitly.
//
// consecutivePLC is owned by the PortAudio output callback for this port:
// each port has its own callback running on its own audio thread, so the
// field is single-writer and does not need atomic semantics. Tests that call
// playoutOneFrame directly are likewise single-threaded with respect to it.
//
// playbackBuffer is retained as a one-shot side channel for TX beep tones
// (see transmit.go beginTransmission/endTransmission); the PortAudio callback
// drains it before falling through to playoutOneFrame so beeps preempt one
// frame of jitter-buffered audio.
type portChannel struct {
	rtpSess           rtpSender
	playbackStream    AudioStream
	sender            *swappableSender
	rtcpSend          *swappableSender
	receiver          *swappableReceiver
	jitter            *rtpJitterBuffer
	playbackBuffer    chan []float32
	cfg               McastPortConfig
	consecutivePLC    int
	lastRemoteRx      atomic.Int64
	playbackUnderruns atomic.Int64
	sendEnabled       atomic.Bool
	receiveEnabled    atomic.Bool
}

// ─── CommsRuntime ─────────────────────────────────────────────────────────────

// CommsRuntime holds live resources allocated by Start. All audio/network
// fields are interfaces so that unit tests can inject fakes without hardware.
type CommsRuntime struct {
	decoder         AudioDecoder
	encoder         AudioEncoder
	broadcastStream AudioStream
	webBridge       *WebAudioBridge
	webEvtSrc       *webEventSource
	localIP         atomic.Pointer[string]
	reopenBroadcast func() error
	broadcastTap    atomic.Pointer[chan []float32]
	ports           []*portChannel
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
	NanoPTTDevicePath        string
	CommKey                  string
	Iface                    string
	ROIPInputDevice          string
	BluetoothInputDevice     string
	BluetoothOutputDevice    string
	BluetoothAudioDeviceHint string
	ControlSource            string
	NanoPTTDeviceName        string
	RtpID                    string
	McastPorts               []McastPortConfig
	ROIPVOXHoldTime          time.Duration
	ROIPMaxTXDuration        time.Duration
	MicGain                  float32
	ROIPVOXThreshold         float32
	ROIPCOSGPIOMask          byte
	EnableNanoPTT            bool
	Debug                    bool
	Loopback                 bool
	Trace                    bool
	Enable                   bool
	EnableBluetoothPtt       bool
	EncoderComplexity        int
	PlaybackLatencyMs        int
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
		ROIPCOSGPIOMask:          cfg.ROIPCOSGPIOMask,
		ROIPVOXThreshold:         cfg.ROIPVOXThreshold,
		ROIPVOXHoldTime:          cfg.ROIPVOXHoldTime,
		ROIPMaxTXDuration:        cfg.ROIPMaxTXDuration,
		ROIPInputDevice:          cfg.ROIPInputDevice,
		EncoderComplexity:        cfg.EncoderComplexity,
		PlaybackLatencyMs:        cfg.PlaybackLatencyMs,
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
			// Open sockets for every talk group so that EnableTalkGroupSend /
			// EnableTalkGroupReceive can activate any port at runtime without
			// a restart. Only port 0 is active on first startup.
			active := i == 0
			cfg.McastPorts[i] = McastPortConfig{
				Address:            tg.Address,
				Port:               tg.Port,
				Send:               true,
				Receive:            true,
				InitSendEnabled:    &active,
				InitReceiveEnabled: &active,
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

	// Apply ROIP-specific defaults after ControlSource is normalised.
	if cfg.ControlSource == controlSourceROIP {
		if cfg.ROIPCOSGPIOMask == 0 && cfg.ROIPVOXThreshold == 0 {
			// Neither explicitly configured: default to COS-primary, VOX fallback.
			cfg.ROIPCOSGPIOMask = roipDefaultCOSMask
			cfg.ROIPVOXThreshold = roipDefaultVOXThresh
		}

		if cfg.ROIPVOXThreshold > 0 && cfg.ROIPVOXHoldTime == 0 {
			cfg.ROIPVOXHoldTime = roipDefaultVOXHold
		}

		if cfg.ROIPMaxTXDuration == 0 {
			cfg.ROIPMaxTXDuration = roipDefaultMaxTX
		}

		if cfg.ROIPInputDevice == "" {
			cfg.ROIPInputDevice = cfg.BluetoothInputDevice
		}
	}

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
	complexity := cfg.EncoderComplexity
	if complexity <= 0 || complexity > 10 {
		complexity = encoderComplexity
	}

	enc, err := newOpusEncoder(complexity)
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
				_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
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

// setMulticastTTL sets the IP multicast TTL on a UDP socket.
func setMulticastTTL(conn *net.UDPConn, ttl int) error {
	if err := ipv4.NewPacketConn(conn).SetMulticastTTL(ttl); err != nil {
		return fmt.Errorf("set multicast TTL: %w", err)
	}

	return nil
}

// rxSocketBufBytes is the requested SO_RCVBUF size for the RTP receive
// socket. 1 MiB absorbs bursty mesh arrivals when receiveLoop is briefly
// scheduled out (GC, scheduler hand-off, neighbor goroutine). At ~100-byte
// Opus payloads this is roughly 10000 frames of headroom — far more than
// any realistic stall. The kernel may clamp the actual value at
// net.core.rmem_max (typically 208 KB on stock Linux, but embedded targets
// usually raise this in sysctl); the post-set verification log records
// what we actually got so undersized rmem_max is observable.
const rxSocketBufBytes = 1 << 20

// getReadBufferBytes returns the kernel's actual SO_RCVBUF for conn. Linux
// reports the doubled value (the kernel adds bookkeeping overhead to
// whatever was requested), so the returned value is typically twice the
// argument passed to SetReadBuffer.
func getReadBufferBytes(conn *net.UDPConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("syscall conn: %w", err)
	}

	var (
		val     int
		sockErr error
	)

	if controlErr := raw.Control(func(fd uintptr) {
		val, sockErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
	}); controlErr != nil {
		return 0, fmt.Errorf("control: %w", controlErr)
	}

	if sockErr != nil {
		return 0, fmt.Errorf("getsockopt SO_RCVBUF: %w", sockErr)
	}

	return val, nil
}

// boolPtrVal dereferences p when non-nil; otherwise returns fallback.
// Used to distinguish "not set" from false in McastPortConfig.Init* fields.
func boolPtrVal(p *bool, fallback bool) bool {
	if p != nil {
		return *p
	}

	return fallback
}

// buildSinglePortChannel opens all sockets and creates the RTP session for one
// McastPortConfig entry. Directions (Send / Receive) are reflected by which
// fields are populated: a Send=false port has nil sender/rtcpSend/rtpSess; a
// Receive=false port has nil receiver. The runtime atomic direction flags are
// initialized from mpc.InitSendEnabled / InitReceiveEnabled (falling back to
// mpc.Send / Receive) so the hot paths can read them without locking.
func (cfg *CommsConfig) buildSinglePortChannel( //nolint:gocognit
	mpc McastPortConfig,
	localIP string,
	ifi *net.Interface,
	ssrc uint32,
) (*portChannel, error) {
	pc := &portChannel{cfg: mpc}
	pc.sendEnabled.Store(boolPtrVal(mpc.InitSendEnabled, mpc.Send))
	pc.receiveEnabled.Store(boolPtrVal(mpc.InitReceiveEnabled, mpc.Receive))

	if mpc.Send {
		// ── RTP sender ─────────────────────────────────────────────────
		dst := &net.UDPAddr{IP: net.ParseIP(mpc.Address), Port: mpc.Port}
		src := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		sendConn, err := net.DialUDP("udp4", src, dst)
		if err != nil {
			return nil, fmt.Errorf("dial RTP sender %s:%d: %w", mpc.Address, mpc.Port, err)
		}

		if errTTL := setMulticastTTL(sendConn, rtpMulticastTTL); errTTL != nil {
			_ = sendConn.Close()

			return nil, fmt.Errorf("set multicast TTL on RTP sender %s:%d: %w", mpc.Address, mpc.Port, errTTL)
		}

		// ── RTCP sender ────────────────────────────────────────────────
		rtcpDst := &net.UDPAddr{IP: net.ParseIP(mpc.Address), Port: mpc.Port + 1}
		rtcpSrc := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		rtcpConn, err := net.DialUDP("udp4", rtcpSrc, rtcpDst)
		if err != nil {
			_ = sendConn.Close()

			return nil, fmt.Errorf("dial RTCP sender %s:%d: %w", mpc.Address, mpc.Port+1, err)
		}

		if errTTL := setMulticastTTL(rtcpConn, rtpMulticastTTL); errTTL != nil {
			_ = sendConn.Close()
			_ = rtcpConn.Close()

			return nil, fmt.Errorf("set multicast TTL on RTCP sender %s:%d: %w", mpc.Address, mpc.Port+1, errTTL)
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

		if err := recvConn.SetReadBuffer(rxSocketBufBytes); err != nil {
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

		// Verify what the kernel actually granted us. Linux clamps SO_RCVBUF
		// at net.core.rmem_max and silently caps the request, so logging the
		// observed value lets an operator see whether sysctl is undersized
		// for the desired audio safety margin.
		if got, err := getReadBufferBytes(recvConn); err == nil {
			cfg.Log.Debug().
				Int("requested_bytes", rxSocketBufBytes).
				Int("actual_bytes", got).
				Str("addr", mpc.Address).
				Int("port", mpc.Port).
				Msg("comms: rx socket buffer")
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
		pc.jitter = newRTPJitterBuffer(jitterPrebufferPackets, jitterMaxDepth)

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

	// playbackBuffer is a small one-shot side channel used by the TX path
	// (transmit.go) to inject start/stop beep tones into the local speaker.
	// It is no longer the carrier for decoded RTP audio — that flows through
	// pc.jitter directly into the PortAudio output callback via
	// playoutOneFrame, eliminating the producer/consumer clock mismatch that
	// previously caused stutter.
	const beepChannelDepth = 4

	// Suggest a playback device buffer depth to PortAudio. This is the only
	// layer of buffering that protects against playback-side OS scheduling
	// stalls — the Go-side jitter buffer sits upstream of the DAC and cannot
	// help once the audio thread is preempted. The callback chunk size stays
	// at frameSize so playoutOneFrame is unchanged.
	//
	// Floor at outDev.DefaultHighOutputLatency: some hardware reports a
	// "high" latency that is essentially the same as one callback period
	// (e.g. 21 ms on the OpenVLM USB audio class device, where the next
	// useful step up is the configured value); other hardware reports a
	// genuinely higher value, in which case we honor the device hint
	// rather than overriding it downward. The host API may still clamp
	// the suggestion — the actual granted latency is logged below.
	playbackLatency := time.Duration(cfg.PlaybackLatencyMs) * time.Millisecond
	if playbackLatency < outDev.DefaultHighOutputLatency {
		playbackLatency = outDev.DefaultHighOutputLatency
	}

	playbackParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outDev,
			Channels: channels,
			Latency:  playbackLatency,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	// Open a dedicated playback stream for every port that has an open
	// receiver socket, regardless of its initial receiveEnabled state.
	// This ensures that EnableTalkGroupReceive can activate any port at
	// runtime without needing a restart.
	for _, pc := range rt.ports {
		if pc.receiver == nil {
			continue
		}

		pc.playbackBuffer = make(chan []float32, beepChannelDepth)

		pcRef := pc // capture for callback closure

		rawPlayback, openErr := portaudio.OpenStream(playbackParams, func(_, out []float32) {
			// Beep injection: TX start/stop tones preempt one frame of
			// jitter-buffered audio. The select is non-blocking so a
			// missing beep falls straight through to playoutOneFrame.
			select {
			case data := <-pcRef.playbackBuffer:
				copy(out, data)

				return
			default:
			}

			cfg.playoutOneFrame(pcRef, rt, pcRef.jitter, out)
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

		// Log the actual output latency the host API granted. This may
		// differ from playbackLatency if the host API clamped the
		// suggestion. Deploy-time verification uses this to confirm
		// whether the configured comms.playbackLatencyMs took effect or
		// fell back to the device's idea of "high latency". The
		// device_high field is the floor we used (so it is obvious when
		// the configured value was overridden by a higher device hint).
		if info := rawPlayback.Info(); info != nil {
			cfg.Log.Debug().
				Int("configured_latency_ms", cfg.PlaybackLatencyMs).
				Dur("device_high_latency", outDev.DefaultHighOutputLatency).
				Dur("requested_latency", playbackLatency).
				Dur("actual_output_latency", info.OutputLatency).
				Int("port", pc.cfg.Port).
				Msg("comms: playback stream opened")
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

	// PortAudio calls this callback every 20 ms with a frameSize (960-sample)
	// float32 buffer captured from the input device. The callback runs on a
	// real-time audio thread, so it must not block or allocate on the heap.
	stream, err := portaudio.OpenStream(inParams, func(in []float32) {
		// Optional tap for ROIP VOX energy monitoring. When set, a copy of the
		// raw input frame is pushed non-blockingly so the VOX loop can detect
		// silence during transmission without a separate PortAudio stream.
		if tapPtr := rt.broadcastTap.Load(); tapPtr != nil {
			fp := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
			f := (*fp)[:frameSize]
			copy(f, in)

			select {
			case *tapPtr <- f:
			default:
				returnFloat32(f)
			}
		}

		// Default gain to unity if the caller left MicGain unset or zero.
		gain := cfg.MicGain
		if gain <= 0 {
			gain = 1.0
		}

		// Borrow a pooled int16 slice for the PCM conversion. Using a pool
		// avoids a per-frame heap allocation on the audio hot path.
		pcmPtr := int16Pool.Get().(*[]int16) //nolint:forcetypeassert
		pcm := (*pcmPtr)[:len(in)]

		// Convert float32 samples [-1.0, 1.0] → int16 [-32767, 32767].
		// MicGain is applied first; the result is hard-clipped to the legal
		// float range before scaling to prevent int16 overflow.
		for i, v := range in {
			v *= gain
			if v > 1.0 {
				v = 1.0
			} else if v < -1.0 {
				v = -1.0
			}

			pcm[i] = int16(v * 32767)
		}

		// Borrow a pooled output buffer for the Opus encoder.
		bufPtr := encBufPool.Get().(*[]byte) //nolint:forcetypeassert
		buf := *bufPtr

		// Encode the int16 PCM frame to Opus. The PCM pool slice is returned
		// immediately after encoding because it is no longer needed.
		n, encErr := rt.encoder.Encode(pcm, buf)

		int16Pool.Put(pcmPtr)

		if encErr != nil {
			encBufPool.Put(bufPtr)

			return
		}

		// Transmit the encoded Opus payload as RTP to every send-enabled port.
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

// buildEventSource constructs the PTT EventSource defined by cfg.ControlSource.
//
// Three backends are supported:
//   - "openvlm" (defaultCtrlSrc): reads PTT state directly from an OpenVLM-compatible
//     USB audio/HID dongle via its HID interrupt endpoint.
//   - "roip": ROIP bridge mode — automatic TX/RX on the same OpenVLM hardware
//     using COS GPIO detection with VOX (audio energy) as fallback.
//   - anything else (default): searches for a matching evdev input device via
//     findCommDevice and wraps it in a NanoPTT source that decodes PTT events
//     using cfg.CommKey.
//
// Returns an error only in the default branch when no matching evdev device is found.
func (cfg *CommsConfig) buildEventSource(rt *CommsRuntime) (EventSource, error) {
	switch cfg.ControlSource {
	case defaultCtrlSrc:
		cfg.Log.Info().Msgf("comms: PTT on OpenVLM HID dongle (VID=0x%04X PID=0x%04X)",
			openvlmVendorID, openvlmProductID)

		return NewOpenVLMSource(cfg.Log), nil

	case controlSourceROIP:
		cfg.Log.Info().Msgf(
			"comms: ROIP bridge on OpenVLM (VID=0x%04X PID=0x%04X) COSmask=0x%02X VOX=%.3f hold=%s",
			openvlmVendorID, openvlmProductID, cfg.ROIPCOSGPIOMask, cfg.ROIPVOXThreshold, cfg.ROIPVOXHoldTime,
		)

		isReceiving := func() bool { return cfg.isReceivingRemote(rt) }
		isBroadcasting := func() bool { return rt.broadcasting.Load() }
		setTap := func(ch chan []float32) { rt.broadcastTap.Store(&ch) }
		clearTap := func() { rt.broadcastTap.Store(nil) }

		return NewROIPSource(cfg, isReceiving, isBroadcasting, setTap, clearTap, cfg.Log), nil

	case controlSourceWeb:
		cfg.Log.Info().Msg("comms: PTT via web RPC")

		ws := NewWebEventSource(cfg.Log)
		rt.webEvtSrc = ws

		return ws, nil

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

// GetWebEventSource returns the webEventSource created when ControlSource is
// "web". Returns nil when comms is not running or a different control source
// is active.
func GetWebEventSource() *webEventSource {
	cfg := activeConfig.Load()
	if cfg == nil || cfg.runtime == nil {
		return nil
	}

	return cfg.runtime.webEvtSrc
}

// GetWebAudioBridge returns the WebAudioBridge created when ControlSource is
// "web". Returns nil when comms is not running or a different control source
// is active.
func GetWebAudioBridge() *WebAudioBridge {
	cfg := activeConfig.Load()
	if cfg == nil || cfg.runtime == nil {
		return nil
	}

	return cfg.runtime.webBridge
}

// startHardwareAudio initializes PortAudio, opens broadcast and playback
// streams, and returns a cleanup function that stops and closes them.
func (cfg *CommsConfig) startHardwareAudio(rt *CommsRuntime) (func(), error) {
	silenceALSAProbeNoise()

	err := portaudio.Initialize()

	restoreALSAErrorHandler()

	if err != nil {
		return nil, fmt.Errorf("comms: failed to initialize PortAudio: %w", err)
	}

	broadcastStream, inDev, audioErr := cfg.buildAudio(rt)
	if audioErr != nil {
		_ = portaudio.Terminate()

		return nil, fmt.Errorf("comms: failed to build audio streams: %w", audioErr)
	}

	rt.broadcastStream = broadcastStream
	rt.reopenBroadcast = func() error { return cfg.reopenBroadcastStream(rt, inDev) }

	for _, pc := range rt.ports {
		if pc.playbackStream != nil {
			if startErr := pc.playbackStream.Start(); startErr != nil {
				_ = broadcastStream.Close()
				_ = portaudio.Terminate()

				return nil, fmt.Errorf("comms: failed to start playback stream: %w", startErr)
			}
		}
	}

	return func() {
		for _, pc := range rt.ports {
			if pc.playbackStream != nil {
				_ = pc.playbackStream.Stop()
				_ = pc.playbackStream.Close()
			}
		}

		_ = broadcastStream.Close()
		_ = portaudio.Terminate()
	}, nil
}

// Start initializes all comms subsystems and blocks until ctx is canceled.
// Returns nil on clean shutdown, or an error if initialization fails.
// The caller is responsible for canceling ctx to stop the subsystem.
func (cfg *CommsConfig) Start(ctx context.Context) error {
	if !cfg.Enable {
		cfg.Log.Info().Msg("comms: functionality disabled; not starting")

		return nil
	}

	cfg.applyDefaults()

	if cfg.ControlSource != controlSourceWeb {
		if cfg.ControlSource == defaultCtrlSrc || cfg.ControlSource == controlSourceROIP {
			detectAndSetALSACard(cfg.Log)
		}
	}

	switch {
	case cfg.Trace:
		cfg.Log = cfg.Log.Level(zerolog.TraceLevel)
	case cfg.Debug:
		cfg.Log = cfg.Log.Level(zerolog.DebugLevel)
	}

	if cfg.Debug && cfg.ControlSource != controlSourceWeb {
		cfg.logInputDeviceList()
	}

	cfg.Log.Info().Msgf(
		"comms: starting iface=%s talkgroups=%d key=%s debug=%t trace=%t loopback=%t device=%s",
		cfg.Iface, len(cfg.McastPorts), cfg.CommKey,
		cfg.Debug, cfg.Trace, cfg.Loopback, cfg.ControlSource,
	)

	// ── codec ──────────────────────────────────────────────────────────────
	enc, dec, err := cfg.buildCodec()
	if err != nil {
		return fmt.Errorf("comms: failed to build Opus codec: %w", err)
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
		return fmt.Errorf("comms: failed to set up network: %w", netErr)
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

		cfg.runtime = nil

		activeConfig.Store((*CommsConfig)(nil))
	}()

	cfg.runtime = rt
	activeConfig.Store(cfg)

	// ── event source ───────────────────────────────────────────────────────
	src, srcErr := cfg.buildEventSource(rt)
	if srcErr != nil {
		return fmt.Errorf("comms: failed to build event source: %w", srcErr)
	}

	// ── audio I/O ─────────────────────────────────────────────────────────
	if cfg.ControlSource == controlSourceWeb {
		// Web mode: skip PortAudio entirely; the browser provides audio I/O.
		rt.webBridge = NewWebAudioBridge(cfg, rt, cfg.Log)
	} else {
		cleanup, hwErr := cfg.startHardwareAudio(rt)
		if hwErr != nil {
			return hwErr
		}

		defer cleanup()
	}

	// ── run loop ───────────────────────────────────────────────────────────
	cfg.Run(ctx, rt, src)

	cfg.Log.Info().Msg("comms: subsystem stopped")

	return nil
}
