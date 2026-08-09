package comms

import (
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/codec"
	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/device"
	"github.com/openmanet/openmanetd/internal/comms/webaudio"
	"github.com/openmanet/openmanetd/internal/config"
)

// ─── CommsRuntime ─────────────────────────────────────────────────────────────

// BroadcastCapture is the unified capture stream interface the runtime
// exposes to the TX path. It extends device.AudioStream (lifecycle
// Start/Stop/Close, called once per StartHardware cycle) with a per-PTT
// SetTxEnabled gate. Captured frames always flow to the optional VOX tap;
// they only flow to the Opus encoder + RTP send when SetTxEnabled(true)
// is the most recent call. The stream is opened once at StartHardware
// and stays open for the lifetime of the comms run.
type BroadcastCapture interface {
	device.AudioStream
	SetTxEnabled(bool)
}

// CommsRuntime holds live resources allocated by Start. All audio/network
// fields are interfaces so that unit tests can inject fakes without hardware.
//
// RemoteRxActive is the cached half-duplex receive flag. The PTT TX path
// reads it via isReceivingRemote in O(1) instead of walking every port's
// HalfDuplexGate. It is set immediately by receiveLoop on every inbound
// packet from a send-enabled port (no false negatives at the start of an
// incoming stream) and cleared by halfDuplexDecayLoop on a coarse 100 ms
// ticker once every gate's window has expired.
type CommsRuntime struct {
	Decoder         codec.AudioDecoder
	Encoder         codec.AudioEncoder
	FECAdapter      *FECAdapter
	WebBridge       *webaudio.Bridge
	WebEvtSrc       *control.WebEventSource
	LocalIP         atomic.Pointer[string]
	BroadcastTap    atomic.Pointer[chan []float32]
	Ports           []*PortChannel
	BeepBufferStart []int16
	BeepBufferStop  []int16
	// PlaybackOutputLatency is the actual output latency the backend
	// granted when the per-port playback streams were opened. The TX
	// path uses it in beginTransmission to delay SetTxEnabled(true)
	// until the start-tone beep has fully emerged from the speaker so
	// an acoustic (or device sidetone) path from speaker → mic cannot
	// pick the beep up and transmit it.
	PlaybackOutputLatency time.Duration
	Broadcasting          atomic.Bool
	RemoteRxActive        atomic.Bool

	// mu protects broadcastStream. It is written at startup by
	// initAudioIO/startHardwareAudio and again by the Run loop's audio
	// recovery path; it is read from the Run goroutine (transmit paths)
	// and from the instrumentation snapshot goroutine, so a plain field
	// is not enough once recovery can install it mid-run.
	mu              sync.RWMutex
	broadcastStream BroadcastCapture
}

// Broadcast returns the live capture stream, or nil when hardware audio is
// not (yet) up. Reads outnumber writes by orders of magnitude, hence RWMutex.
func (rt *CommsRuntime) Broadcast() BroadcastCapture {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	return rt.broadcastStream
}

// SetBroadcast installs the capture stream produced by a successful
// hardware audio init (startup or in-run recovery).
func (rt *CommsRuntime) SetBroadcast(bs BroadcastCapture) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	rt.broadcastStream = bs
}

// ─── CommsConfig ──────────────────────────────────────────────────────────────

// CommsConfig holds the static configuration for the comms subsystem.
// Allocate one with NewComms and call Start to begin operation. All
// exported fields must be set before Start is called.
//
// CommsConfig is treated as immutable after Start: the live runtime is
// owned by *Service (returned by Start via SetDefault) so the static
// config and the per-startup runtime have distinct lifetimes.
type CommsConfig struct {
	Log                      zerolog.Logger
	AuxHandler               control.AuxEventHandler
	Interrupt                chan os.Signal
	startHardwareAudioFn     func(rt *CommsRuntime) (func(), error)
	audioInitRetryDelay      time.Duration
	CommKey                  string
	BluetoothInputDevice     string
	Iface                    string
	BluetoothAudioDeviceHint string
	ControlSource            string
	NanoPTTDeviceName        string
	RtpID                    string
	NanoPTTDevicePath        string
	BluetoothOutputDevice    string
	McastPorts               []McastPortConfig
	ROIPMaxTXDuration        time.Duration
	ROIPVOXHoldTime          time.Duration
	EncoderComplexity        int
	PttStartDelayMs          int
	CaptureFramesPerBuffer   int
	CaptureLatencyMs         int
	PlaybackLatencyMs        int
	PacketLossPerc           int
	HalfDuplexThreshold      time.Duration
	ROIPVOXThreshold         float32
	MicGain                  float32
	EnableNanoPTT            bool
	EnableBluetoothPtt       bool
	Enable                   bool
	Trace                    bool
	Loopback                 bool
	Debug                    bool
	ROIPCOSGPIOMask          byte
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
		HalfDuplexThreshold:      cfg.HalfDuplexThreshold,
		EncoderComplexity:        cfg.EncoderComplexity,
		PacketLossPerc:           cfg.PacketLossPerc,
		PlaybackLatencyMs:        cfg.PlaybackLatencyMs,
		CaptureLatencyMs:         cfg.CaptureLatencyMs,
		CaptureFramesPerBuffer:   cfg.CaptureFramesPerBuffer,
		PttStartDelayMs:          cfg.PttStartDelayMs,
		AuxHandler:               cfg.AuxHandler,
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
			cfg.ROIPCOSGPIOMask = control.ROIPDefaultCOSMask
			cfg.ROIPVOXThreshold = control.ROIPDefaultVOXThresh
		}

		if cfg.ROIPVOXThreshold > 0 && cfg.ROIPVOXHoldTime == 0 {
			cfg.ROIPVOXHoldTime = control.ROIPDefaultVOXHold
		}

		if cfg.ROIPMaxTXDuration == 0 {
			cfg.ROIPMaxTXDuration = control.ROIPDefaultMaxTX
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

	if cfg.audioInitRetryDelay == 0 {
		cfg.audioInitRetryDelay = defaultAudioInitRetryDelay
	}
}
