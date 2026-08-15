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
	buildFn func() *CommsConfig
	startFn func(*CommsConfig) startFunc
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	running bool
}

// NewCommsManager creates a new CommsManager. The manager is created at startup
// regardless of whether comms is enabled, so the API handler always has it.
func NewCommsManager(cfg *config.Config, logger zerolog.Logger) *CommsManager {
	m := &CommsManager{
		cfg:    cfg,
		logger: logger,
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
	})
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
