package comms

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/control/alsa"
	"github.com/openmanet/openmanetd/internal/config"
)

// CommsLifecycle defines the interface for managing comms runtime lifecycle.
// The handler depends on this interface so that tests can provide a mock.
type CommsLifecycle interface {
	Enable() error
	Disable()
	IsRunning() bool
}

// startFunc is the signature matching CommsConfig.Start for testability.
type startFunc func(ctx context.Context) error

// CommsManager owns the comms lifecycle and serializes enable/disable operations.
// It implements CommsLifecycle.
type CommsManager struct {
	cfg     *config.Config
	logger  zerolog.Logger
	mixer   *alsa.Volume
	buildFn func() *CommsConfig
	startFn func(*CommsConfig) startFunc
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	running bool
}

// NewCommsManager creates a new CommsManager. The manager is created at
// startup regardless of whether comms is enabled, so the API handler
// always has it. mixer is the shared hardware mixer accessor (also used
// by the CommsService audio-mixer RPCs); it may be nil in tests.
func NewCommsManager(cfg *config.Config, logger zerolog.Logger, mixer *alsa.Volume) *CommsManager {
	m := &CommsManager{
		cfg:    cfg,
		logger: logger,
		mixer:  mixer,
		startFn: func(cc *CommsConfig) startFunc {
			return cc.Start
		},
	}
	m.buildFn = m.buildCommsConfig

	return m
}

// buildCommsConfig reads the current configuration and builds a CommsConfig.
func (m *CommsManager) buildCommsConfig() *CommsConfig {
	return NewComms(CommsConfig{
		Log:                      m.logger,
		Enable:                   true, // manager only calls Start when enabling
		Iface:                    m.cfg.GetMeshNetInterface(),
		Debug:                    m.cfg.GetCommsDebug(),
		Loopback:                 m.cfg.GetCommsLoopback(),
		Trace:                    m.cfg.GetCommsTrace(),
		ControlSource:            m.cfg.GetCommsControlSource(),
		MicGain:                  m.cfg.GetCommsMicGain(),
		EnableNanoPTT:            m.cfg.GetCommsNanoPTTEnable(),
		NanoPTTDevicePath:        m.cfg.GetCommsNanoPTTDevicePath(),
		NanoPTTDeviceName:        m.cfg.GetCommsNanoPTTDeviceName(),
		EnableBluetoothPtt:       m.cfg.GetCommsBluetoothPttEnable(),
		BluetoothAudioDeviceHint: m.cfg.GetCommsBluetoothPttBluetoothAudioDeviceHint(),
		BluetoothInputDevice:     m.cfg.GetCommsBluetoothPttBluetoothInputDevice(),
		BluetoothOutputDevice:    m.cfg.GetCommsBluetoothPttBluetoothOutputDevice(),
		EncoderComplexity:        m.cfg.GetCommsEncoderComplexity(),
		PacketLossPerc:           m.cfg.GetCommsPacketLossPerc(),
		DSCP:                     m.cfg.GetCommsDSCP(),
		PlaybackLatencyMs:        m.cfg.GetCommsPlaybackLatencyMs(),
		CaptureLatencyMs:         m.cfg.GetCommsCaptureLatencyMs(),
		CaptureFramesPerBuffer:   m.cfg.GetCommsCaptureFramesPerBuffer(),
		AuxHandler: &alsa.Controller{
			Log: m.logger.With().Str("subsystem", "alsa-vol").Logger(),
		},
		AudioMixerStartup: m.mixerStartup(),
	})
}

// mixerStartupUpdate translates persisted comms.audio values into an
// alsa.Update. ok is false when no comms.audio key is set.
func mixerStartupUpdate(cfg *config.Config) (alsa.Update, bool) {
	if !cfg.HasCommsAudioSettings() {
		return alsa.Update{}, false
	}

	var u alsa.Update

	if v := cfg.GetCommsAudioSpeakerVolume(); v >= 0 {
		u.SpeakerPct = &v
	}

	if v := cfg.GetCommsAudioMicVolume(); v >= 0 {
		u.MicPct = &v
	}

	if enabled, set := cfg.GetCommsAudioAGC(); set {
		u.AGC = &enabled
	}

	return u, true
}

// mixerStartup returns the startup mixer re-apply closure, or nil when no
// comms.audio key is set (the daemon then never touches the hardware
// mixer). The config is re-read at invocation time so API-persisted
// values from the current run are picked up by later recoveries.
func (m *CommsManager) mixerStartup() func() {
	if m.mixer == nil {
		return nil
	}

	if _, ok := mixerStartupUpdate(m.cfg); !ok {
		return nil
	}

	cfg := m.cfg
	mixer := m.mixer

	return func() {
		u, ok := mixerStartupUpdate(cfg)
		if !ok {
			return
		}

		mixer.ApplyStartup(context.Background(), u)
	}
}

// Enable starts the comms subsystem. It is idempotent: if comms is already
// running it returns nil. The subsystem runs in a background goroutine and
// can be stopped with Disable. Validate is invoked synchronously so an
// invalid ControlSource is reported to the caller immediately rather than
// surfacing later as an asynchronous error inside the background goroutine.
func (m *CommsManager) Enable() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	cc := m.buildFn()

	if err := cc.Validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	start := m.startFn(cc)

	go func() {
		defer close(done)

		if err := start(ctx); err != nil {
			m.logger.Error().Err(err).Msg("comms: subsystem exited with error")
		}
	}()

	m.cancel = cancel
	m.done = done
	m.running = true
	m.logger.Info().Msg("comms: subsystem enabled")

	return nil
}

// Disable stops the comms subsystem and waits for cleanup to finish.
// It is idempotent: no-op if not running.
func (m *CommsManager) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.cancel()
	<-m.done

	m.cancel = nil
	m.done = nil
	m.running = false
	m.logger.Info().Msg("comms: subsystem disabled")
}

// IsRunning reports whether the comms subsystem is currently active.
func (m *CommsManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.running
}
