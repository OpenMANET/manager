package comms

import (
	"os"
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
	BroadcastStream BroadcastCapture
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
	Interrupt                chan os.Signal
	NanoPTTDevicePath        string
	CommKey                  string
	Iface                    string
	BluetoothInputDevice     string
	BluetoothOutputDevice    string
	BluetoothAudioDeviceHint string
	ControlSource            string
	NanoPTTDeviceName        string
	RtpID                    string
	McastPorts               []McastPortConfig
	ROIPVOXHoldTime          time.Duration
	ROIPMaxTXDuration        time.Duration
	HalfDuplexThreshold      time.Duration
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
	// PacketLossPerc is the Opus encoder's initial packet-loss-perc hint
	// (LBRR bitrate allocation). Also acts as the floor for the FEC
	// adapter — the adapter is free to raise above this value but will
	// never drop below it. Zero or out-of-range → default (20).
	PacketLossPerc    int
	PlaybackLatencyMs int
	CaptureLatencyMs  int
	// CaptureFramesPerBuffer is the per-callback frame count suggested to
	// malgo as DeviceConfig.PeriodSizeInFrames. 960 matches the Opus frame
	// size so every callback produces exactly one RTP packet. A value of 0
	// is translated to the default audiopool.FrameSize (960); a negative
	// value lets miniaudio choose a period aligned with the native ALSA
	// period. Either way the capture callback still emits 20 ms frames
	// downstream via the captureChunker accumulation step. Defaults to
	// audiopool.FrameSize when the config layer supplies a zero value
	// through a non-viper path (e.g. tests constructing CommsConfig
	// directly).
	CaptureFramesPerBuffer int
	// PttStartDelayMs bounds how long beginTransmission waits between
	// queueing the start-tone beep and starting the mic capture stream. The
	// malgo playback callback drains the beep buffer before falling through
	// to playoutOneFrame, so the delay is only required to give hardware
	// that warms its mic stream slowly time to settle before the first
	// encoded frame goes out. Defaults to defaultPttStartDelayMs (50 ms)
	// when ≤ 0; set to 0 explicitly to skip the wait entirely.
	PttStartDelayMs int
}

const bs22SCODeviceSpec = "bt_sco"

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

	// BS-22 audio is stable only on the SCO profile in the current stack.
	// Force SCO routing regardless of hint/legacy values so operators do not
	// end up on A2DP/default devices and get unusable audio.
	if cfg.ControlSource == controlSourceBS22 {
		cfg.BluetoothAudioDeviceHint = bs22SCODeviceSpec
		cfg.BluetoothInputDevice = bs22SCODeviceSpec
		cfg.BluetoothOutputDevice = bs22SCODeviceSpec
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
