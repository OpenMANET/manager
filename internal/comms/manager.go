package comms

import (
	"context"
	"sync"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
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
//
// Interface resolution:
//   - Iface (L2 multicast-join interface) prefers comms.iface, falling
//     back to meshNetInterface.
//   - LocalIPIface (L3 interface with the IPv4 address) prefers
//     comms.localIPIface, falling back to meshNetInterface.
//
// On batman-adv deployments the right setting is usually:
//   comms.iface:        bat0
//   comms.localIPIface: br-ahwlan
//
// because bat0 carries multicast RTP at L2 but has no IPv4 address,
// while the bridge is the L3 interface with the host's IP.
func (m *CommsManager) buildCommsConfig() *CommsConfig {
	iface := m.cfg.GetCommsIface()
	if iface == "" {
		iface = m.cfg.GetMeshNetInterface()
	}

	localIPIface := m.cfg.GetCommsLocalIPIface()
	if localIPIface == "" {
		localIPIface = m.cfg.GetMeshNetInterface()
	}

	return NewComms(CommsConfig{
		Log:                      m.logger,
		Enable:                   true, // manager only calls Start when enabling
		Iface:                    iface,
		LocalIPIface:             localIPIface,
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
		PlaybackLatencyMs:        m.cfg.GetCommsPlaybackLatencyMs(),
		CaptureLatencyMs:         m.cfg.GetCommsCaptureLatencyMs(),
		CaptureFramesPerBuffer:   m.cfg.GetCommsCaptureFramesPerBuffer(),
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
