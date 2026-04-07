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

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/device"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
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
	Int16Pool = sync.Pool{ //nolint:gochecknoglobals
		New: func() any {
			s := make([]int16, frameSize)

			return &s
		},
	}
	// float32Pool has been moved to pools.go (sibling file) so future
	// sub-packages (audio/, control/roip.go) can import it via the parent
	// package without cross-importing each other.
	EncBufPool = sync.Pool{ //nolint:gochecknoglobals
		New: func() any {
			s := make([]byte, encBufSize)

			return &s
		},
	}
)

// returnFloat32 returns a pooled []float32 slice to audiopool.Float32Pool.
// Non-pooled slices (e.g. beep buffers) are silently ignored because their
// capacity will differ from FrameSize.
func returnFloat32(s []float32) {
	audiopool.ReturnFloat32(s)
}

// ReturnInt16 returns a pooled []int16 slice to Int16Pool. Non-pooled slices
// (capacity != frameSize) are silently ignored.
func ReturnInt16(s []int16) {
	if cap(s) != frameSize {
		return
	}

	sp := &s
	Int16Pool.Put(sp)
}

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

// ─── PortChannel ─────────────────────────────────────────────────────────────

// PortChannel holds all live resources for one McastPortConfig entry.
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
type PortChannel struct {
	RTPSess           RTPSender
	PlaybackStream    AudioStream
	Sender            *SwappableSender
	RTCPSend          *SwappableSender
	Receiver          *SwappableReceiver
	Jitter            *RTPJitterBuffer
	PlaybackBuffer    chan []int16
	cfg               McastPortConfig
	ConsecutivePLC    int
	RxGate            HalfDuplexGate
	PlaybackUnderruns atomic.Int64
	SendEnabled       atomic.Bool
	ReceiveEnabled    atomic.Bool
}

// closePartial closes any sockets and the RTP session that this PortChannel
// has acquired so far. It is safe to call on a nil receiver and on a
// PortChannel where some fields are still nil — used both as the rollback
// path inside buildSinglePortChannel and as the bulk cleanup path in
// buildNetwork when a later port fails.
func (pc *PortChannel) closePartial() {
	if pc == nil {
		return
	}

	if pc.Receiver != nil {
		_ = pc.Receiver.Close()
	}

	if s, ok := pc.RTPSess.(*RTPSession); ok && s != nil {
		_ = s.Close()
	}

	if pc.Sender != nil {
		_ = pc.Sender.Close()
	}

	if pc.RTCPSend != nil {
		_ = pc.RTCPSend.Close()
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
//
// On any failure the deferred rollback closes whichever sockets and sessions
// were already attached to pc and returns (nil, err) to the caller; the
// individual error sites only need to assign err and bare-return.
func (cfg *CommsConfig) buildSinglePortChannel(
	mpc McastPortConfig,
	localIP string,
	ifi *net.Interface,
	ssrc uint32,
) (pc *PortChannel, err error) {
	pc = &PortChannel{cfg: mpc}
	pc.RxGate.Threshold = cfg.HalfDuplexThreshold
	pc.SendEnabled.Store(boolPtrVal(mpc.InitSendEnabled, mpc.Send))
	pc.ReceiveEnabled.Store(boolPtrVal(mpc.InitReceiveEnabled, mpc.Receive))

	defer func() {
		if err != nil {
			pc.closePartial()
			pc = nil
		}
	}()

	if mpc.Send {
		// ── RTP sender ─────────────────────────────────────────────────
		dst := &net.UDPAddr{IP: net.ParseIP(mpc.Address), Port: mpc.Port}
		src := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		sendConn, dialErr := net.DialUDP("udp4", src, dst)
		if dialErr != nil {
			err = fmt.Errorf("dial RTP sender %s:%d: %w", mpc.Address, mpc.Port, dialErr)

			return
		}

		pc.Sender = rtp.NewSwappableSender(sendConn)

		if ttlErr := setMulticastTTL(sendConn, rtpMulticastTTL); ttlErr != nil {
			err = fmt.Errorf("set multicast TTL on RTP sender %s:%d: %w", mpc.Address, mpc.Port, ttlErr)

			return
		}

		// ── RTCP sender ────────────────────────────────────────────────
		rtcpDst := &net.UDPAddr{IP: net.ParseIP(mpc.Address), Port: mpc.Port + 1}
		rtcpSrc := &net.UDPAddr{IP: net.ParseIP(localIP), Port: 0}

		rtcpConn, dialErr := net.DialUDP("udp4", rtcpSrc, rtcpDst)
		if dialErr != nil {
			err = fmt.Errorf("dial RTCP sender %s:%d: %w", mpc.Address, mpc.Port+1, dialErr)

			return
		}

		pc.RTCPSend = rtp.NewSwappableSender(rtcpConn)

		if ttlErr := setMulticastTTL(rtcpConn, rtpMulticastTTL); ttlErr != nil {
			err = fmt.Errorf("set multicast TTL on RTCP sender %s:%d: %w", mpc.Address, mpc.Port+1, ttlErr)

			return
		}

		sess, sessErr := rtp.NewSession(ssrc, pc.Sender, pc.RTCPSend, cfg.Log)
		if sessErr != nil {
			err = fmt.Errorf("pion RTP session for %s:%d: %w", mpc.Address, mpc.Port, sessErr)

			return
		}

		pc.RTPSess = sess

		cfg.Log.Debug().Msgf("comms: RTP sender %s:%d  RTCP %s:%d", mpc.Address, mpc.Port, mpc.Address, mpc.Port+1)
	}

	if mpc.Receive {
		// ── RTP receiver ────────────────────────────────────────────────
		// SO_REUSEPORT lets UpdateMulticastEndpoint open a replacement socket
		// on the same port while the current receiver is still running.
		recvConn, listenErr := listenRTPReceiver(&net.UDPAddr{IP: net.IPv4zero, Port: mpc.Port})
		if listenErr != nil {
			err = fmt.Errorf("listen RTP receiver %s:%d: %w", mpc.Address, mpc.Port, listenErr)

			return
		}

		pc.Receiver = rtp.NewSwappableReceiver(recvConn)
		pc.Jitter = rtp.NewJitterBuffer(rtp.PrebufferPackets, rtp.MaxDepth)

		if bufErr := recvConn.SetReadBuffer(rxSocketBufBytes); bufErr != nil {
			err = fmt.Errorf("set RTP read buffer: %w", bufErr)

			return
		}

		// Verify what the kernel actually granted us. Linux clamps SO_RCVBUF
		// at net.core.rmem_max and silently caps the request, so logging the
		// observed value lets an operator see whether sysctl is undersized
		// for the desired audio safety margin.
		if got, gErr := getReadBufferBytes(recvConn); gErr == nil {
			cfg.Log.Debug().
				Int("requested_bytes", rxSocketBufBytes).
				Int("actual_bytes", got).
				Str("addr", mpc.Address).
				Int("port", mpc.Port).
				Msg("comms: rx socket buffer")
		}

		if joinErr := device.JoinMulticastGroup(ifi, recvConn, net.ParseIP(mpc.Address)); joinErr != nil {
			err = joinErr

			return
		}

		cfg.Log.Debug().Msgf("comms: RTP receiver port %d", mpc.Port)
	}

	return pc, nil
}

// buildNetwork opens sockets for every McastPortConfig entry and returns the
// assembled PortChannel slice plus the local IP address of cfg.Iface.
//
// The SSRC used for all Send-enabled ports is derived from cfg.RtpID (or
// localIP as fallback), keeping transmissions from this node identifiable
// across talk groups.
func (cfg *CommsConfig) buildNetwork() ([]*PortChannel, string, error) {
	localIP, ifi, err := device.IfaceIPv4(cfg.Iface)
	if err != nil {
		return nil, "", err
	}

	cfg.Log.Debug().Msgf("comms: interface %s localIP %s", cfg.Iface, localIP)

	rtpID := cfg.RtpID
	if rtpID == "" {
		rtpID = localIP
	}

	ssrc := rtp.SSRCFromID(rtpID)

	ports := make([]*PortChannel, 0, len(cfg.McastPorts))

	for _, mpc := range cfg.McastPorts {
		pc, err := cfg.buildSinglePortChannel(mpc, localIP, ifi, ssrc)
		if err != nil {
			// Clean up already-built channels before propagating the error.
			for _, built := range ports {
				built.closePartial()
			}

			return nil, "", err
		}

		ports = append(ports, pc)
	}

	return ports, localIP, nil
}

// ─── buildAudio ───────────────────────────────────────────────────────────────

// buildAudio resolves PortAudio devices, opens a dedicated playback stream for
// every Receive-capable port (storing it in PortChannel.PlaybackStream), and
// opens the shared broadcast capture stream. Per-port playback streams are
// accessible via rt.Ports after this call returns.
func (cfg *CommsConfig) buildAudio(rt *CommsRuntime) (
	broadcast AudioStream,
	inDev *portaudio.DeviceInfo,
	err error,
) {
	outDev, err := device.ResolveAudio(cfg.BluetoothOutputDevice, false)
	if err != nil {
		return nil, nil, err
	}

	inDev, err = device.ResolveAudio(cfg.BluetoothInputDevice, true)
	if err != nil {
		return nil, nil, err
	}

	cfg.Log.Info().Msgf("comms: audio in=%s out=%s", inDev.Name, outDev.Name)

	// playbackBuffer is a small one-shot side channel used by the TX path
	// (transmit.go) to inject start/stop beep tones into the local speaker.
	// It is no longer the carrier for decoded RTP audio — that flows through
	// pc.Jitter directly into the PortAudio output callback via
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
	for _, pc := range rt.Ports {
		if pc.Receiver == nil {
			continue
		}

		pc.PlaybackBuffer = make(chan []int16, beepChannelDepth)

		pcRef := pc // capture for callback closure

		// Phase 5: open the playback stream with an int16 callback so
		// PortAudio delivers samples in the native codec format. The
		// gordonklaus/portaudio binding chooses the C sample format
		// (paInt16) from the callback signature via reflection.
		rawPlayback, openErr := portaudio.OpenStream(playbackParams, func(_, out []int16) {
			// Beep injection: TX start/stop tones preempt one frame of
			// jitter-buffered audio. The select is non-blocking so a
			// missing beep falls straight through to playoutOneFrame.
			select {
			case data := <-pcRef.PlaybackBuffer:
				copy(out, data)

				return
			default:
			}

			cfg.playoutOneFrame(pcRef, rt, pcRef.Jitter, out)
		})
		if openErr != nil {
			// Close already-opened per-port streams before propagating error.
			for _, built := range rt.Ports {
				if built.PlaybackStream != nil {
					_ = built.PlaybackStream.Close()
					built.PlaybackStream = nil
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

		pc.PlaybackStream = &portaudioStream{rawPlayback}
	}

	broadcast, err = cfg.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		for _, pc := range rt.Ports {
			if pc.PlaybackStream != nil {
				_ = pc.PlaybackStream.Close()
				pc.PlaybackStream = nil
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
	for _, pc := range rt.Ports {
		if !pc.SendEnabled.Load() || pc.RTPSess == nil {
			continue
		}

		if err := pc.RTPSess.Send(payload); err != nil {
			cfg.Log.Debug().Err(err).
				Str("addr", pc.cfg.Address).
				Int("port", pc.cfg.Port).
				Msg("comms: RTP send failed")
		}
	}
}

// openBroadcastStreamOn creates a PortAudio capture stream that encodes mic
// audio via Opus and transmits it as RTP to all send-enabled ports via
// sendToAllPorts. The actual encode and RTP send run on a dedicated goroutine
// inside broadcastEncoder, NOT on the PortAudio audio callback thread, so
// encoder spikes / GC pauses / UDP backpressure cannot starve the audio thread
// and cause ADC overruns at the device.
func (cfg *CommsConfig) openBroadcastStreamOn(inDev *portaudio.DeviceInfo, rt *CommsRuntime) (AudioStream, error) {
	// Phase 3 unified discovery: report the current CM108 descriptor count
	// so the broadcast stream open has the same observable device state as
	// the HID (PTT) side. PortAudio is not enumerable from /sys, so the
	// chosen PortAudio device is still supplied by inDev — the walk here is
	// informational and shares the same code path as openvlmSource. Gated
	// behind Debug so production reopens (e.g. after a stale handle on
	// PTTDown) skip the syscall and the descriptor-slice allocation.
	if cfg.Debug {
		if descs, dErr := device.DiscoverCM108(os.DirFS("/sys"), nil); dErr == nil {
			cfg.Log.Debug().
				Int("cm108_count", len(descs)).
				Str("pa_device", inDev.Name).
				Msg("comms: unified CM108 descriptor scan at broadcast open")
		}
	}

	// Suggest a capture device buffer depth to PortAudio. Symmetric to the
	// playback stream in buildAudio. Floor at inDev.DefaultHighInputLatency
	// so we never undercut the host API's recommendation. The host API may
	// still clamp the suggestion — the actual granted latency is logged
	// below.
	captureLatency := time.Duration(cfg.CaptureLatencyMs) * time.Millisecond
	if captureLatency < inDev.DefaultHighInputLatency {
		captureLatency = inDev.DefaultHighInputLatency
	}

	inParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inDev,
			Channels: channels,
			Latency:  captureLatency,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	be, err := newBroadcastEncoder(cfg, rt, inParams)
	if err != nil {
		return nil, err
	}

	// Log the actual input latency the host API granted. Mirrors the
	// playback stream open log so deploy-time verification has the same
	// fields on both sides. encode_chan_depth makes the new goroutine-based
	// architecture self-documenting on first deploy.
	if info := be.s.Info(); info != nil {
		cfg.Log.Debug().
			Int("configured_latency_ms", cfg.CaptureLatencyMs).
			Dur("device_high_latency", inDev.DefaultHighInputLatency).
			Dur("requested_latency", captureLatency).
			Dur("actual_input_latency", info.InputLatency).
			Int("encode_chan_depth", broadcastEncoderChanDepth).
			Msg("comms: broadcast stream opened")
	}

	return be, nil
}

// reopenBroadcastStream closes the current broadcast stream and opens a new one.
func (cfg *CommsConfig) reopenBroadcastStream(rt *CommsRuntime, inDev *portaudio.DeviceInfo) error {
	if inDev == nil {
		return errors.New("input device is not set")
	}

	if rt.BroadcastStream != nil {
		_ = rt.BroadcastStream.Close()
		rt.BroadcastStream = nil
	}

	stream, err := cfg.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		return err
	}

	rt.BroadcastStream = stream

	return nil
}

// ─── buildEventSource ─────────────────────────────────────────────────────────

// buildEventSource constructs the PTT EventSource for cfg.ControlSource by
// looking the name up in the control-source registry. The four supported
// backends — "openvlm", "roip", "web", "nanoptt" — register themselves via
// init() in control_register.go. Validate() (called from CommsManager.Enable)
// rejects unknown sources up front; this function returns an error if a
// caller still reaches it with an unregistered source.
func (cfg *CommsConfig) buildEventSource(rt *CommsRuntime) (EventSource, error) {
	factory, ok := controlLookup(cfg.ControlSource)
	if !ok {
		return nil, fmt.Errorf("comms: unknown ControlSource %q", cfg.ControlSource)
	}

	deps, err := cfg.buildControlDeps(rt)
	if err != nil {
		return nil, err
	}

	es, err := factory(deps)
	if err != nil {
		return nil, err
	}

	return es, nil
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
	pc := rt.Ports[0]

	if pc.Receiver != nil && newReceiver != nil {
		old := pc.Receiver.Swap(newReceiver)
		_ = old.Close()
	}

	if pc.Sender != nil && newSender != nil {
		// Deferred close: the lock-free Write path on SwappableSender
		// cannot be drained synchronously, so the previous underlying
		// connection is closed after rtp.SwapCloseGrace to let any in-flight
		// sendto(2) on the old fd finish first.
		pc.Sender.SwapAndDeferClose(newSender)
	}

	if pc.RTCPSend != nil && newRTCPSender != nil {
		pc.RTCPSend.SwapAndDeferClose(newRTCPSender)
	}

	rt.LocalIP.Store(&newLocalIP)
}

// Accessors for the active Service live in service.go.
//
// ─── Start ────────────────────────────────────────────────────────────────────

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

	rt.BroadcastStream = broadcastStream
	rt.ReopenBroadcast = func() error { return cfg.reopenBroadcastStream(rt, inDev) }

	for _, pc := range rt.Ports {
		if pc.PlaybackStream != nil {
			if startErr := pc.PlaybackStream.Start(); startErr != nil {
				_ = broadcastStream.Close()
				_ = portaudio.Terminate()

				return nil, fmt.Errorf("comms: failed to start playback stream: %w", startErr)
			}
		}
	}

	return func() {
		for _, pc := range rt.Ports {
			if pc.PlaybackStream != nil {
				_ = pc.PlaybackStream.Stop()
				_ = pc.PlaybackStream.Close()
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
			control.DetectAndSetALSACard(cfg.Log)
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
	// Phase 5: beep buffers are int16-native so they can be written directly
	// into the PortAudio int16 playback callback without an extra conversion.
	// Amplitude 0.2 * 32767 ≈ 6553 matches the previous float32 volume.
	beepStart := make([]int16, frameSize)
	beepStop := make([]int16, frameSize)

	const beepAmp = 0.2 * 32767

	for i := range beepStart {
		beepStart[i] = int16(math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate)) * beepAmp)
		beepStop[i] = int16(math.Sin(2*math.Pi*600*float64(i)/float64(sampleRate)) * beepAmp)
	}

	// ── network ────────────────────────────────────────────────────────────
	ports, localIP, netErr := cfg.buildNetwork()
	if netErr != nil {
		return fmt.Errorf("comms: failed to set up network: %w", netErr)
	}

	// ── assemble runtime ───────────────────────────────────────────────────
	rt := &CommsRuntime{
		Encoder:         enc,
		Decoder:         dec,
		Ports:           ports,
		BeepBufferStart: beepStart,
		BeepBufferStop:  beepStop,
	}

	rt.LocalIP.Store(&localIP)

	defer func() {
		for _, pc := range rt.Ports {
			if pc.Receiver != nil {
				_ = pc.Receiver.Close()
			}

			if pc.RTPSess != nil {
				if s, ok := pc.RTPSess.(*RTPSession); ok {
					_ = s.Close()
				}
			}
		}

		cfg.runtime = nil

		SetDefault(nil)
	}()

	cfg.runtime = rt
	SetDefault(cfg)

	// ── event source ───────────────────────────────────────────────────────
	src, srcErr := cfg.buildEventSource(rt)
	if srcErr != nil {
		return fmt.Errorf("comms: failed to build event source: %w", srcErr)
	}

	// ── audio I/O ─────────────────────────────────────────────────────────
	if cfg.ControlSource == controlSourceWeb {
		// Web mode: skip PortAudio entirely; the browser provides audio I/O.
		rt.WebBridge = NewWebAudioBridge(cfg, rt, cfg.Log)
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
