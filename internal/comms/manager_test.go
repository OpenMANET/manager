package comms

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/comms/control/alsa"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStartFunc returns a startFunc that blocks until ctx is canceled and
// records whether it was called.
func fakeStartFunc(called *atomic.Bool) func(*CommsConfig) startFunc {
	return func(_ *CommsConfig) startFunc {
		return func(ctx context.Context) error {
			called.Store(true)
			<-ctx.Done()

			return nil
		}
	}
}

func newTestManager() (*CommsManager, *atomic.Bool) {
	var called atomic.Bool

	m := &CommsManager{
		logger:  zerolog.Nop(),
		buildFn: func() *CommsConfig { return &CommsConfig{} },
		startFn: fakeStartFunc(&called),
	}

	return m, &called
}

func TestCommsManager_EnableDisable(t *testing.T) {
	m, called := newTestManager()

	if err := m.Enable(); err != nil {
		t.Fatalf("Enable() error: %v", err)
	}

	// Give goroutine a moment to start
	time.Sleep(20 * time.Millisecond)

	if !called.Load() {
		t.Fatal("expected start function to be called")
	}

	if !m.IsRunning() {
		t.Fatal("expected IsRunning() to be true after Enable")
	}

	m.Disable()

	if m.IsRunning() {
		t.Fatal("expected IsRunning() to be false after Disable")
	}
}

func TestCommsManager_EnableIdempotent(t *testing.T) {
	m, _ := newTestManager()

	if err := m.Enable(); err != nil {
		t.Fatalf("first Enable() error: %v", err)
	}

	defer m.Disable()

	// Second Enable should be a no-op
	if err := m.Enable(); err != nil {
		t.Fatalf("second Enable() error: %v", err)
	}

	if !m.IsRunning() {
		t.Fatal("expected IsRunning() to be true")
	}
}

func TestCommsManager_DisableIdempotent(t *testing.T) {
	m, _ := newTestManager()

	// Disable when not running — should not panic or block
	m.Disable()

	if m.IsRunning() {
		t.Fatal("expected IsRunning() to be false")
	}
}

func TestCommsManager_EnableAfterDisable(t *testing.T) {
	m, called := newTestManager()

	if err := m.Enable(); err != nil {
		t.Fatalf("Enable() error: %v", err)
	}

	m.Disable()

	called.Store(false)

	// Re-enable
	if err := m.Enable(); err != nil {
		t.Fatalf("re-Enable() error: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if !called.Load() {
		t.Fatal("expected start function to be called on re-enable")
	}

	if !m.IsRunning() {
		t.Fatal("expected IsRunning() to be true after re-enable")
	}

	m.Disable()
}

func TestCommsManager_DisableEnablePicksUpConfigChange(t *testing.T) {
	// Set up a real Config backed by a temp YAML file so PersistCommsConfig works.
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte("comms:\n  enable: true\n  controlSource: openvlm\n"), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)
	require.NoError(t, v.ReadInConfig())

	cfg := config.NewWithoutWatch(v)

	// Track the CommsConfig received by startFn on each Enable() call.
	var lastControlSource atomic.Value

	m := NewCommsManager(cfg, zerolog.Nop(), nil)
	m.startFn = func(cc *CommsConfig) startFunc {
		return func(ctx context.Context) error {
			lastControlSource.Store(cc.ControlSource)
			<-ctx.Done()

			return nil
		}
	}

	// First Enable — should read "openvlm" from config.
	require.NoError(t, m.Enable())
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, "openvlm", lastControlSource.Load())

	// Disable, change config, re-enable.
	m.Disable()

	require.NoError(t, cfg.PersistCommsConfig(true, "web"))

	require.NoError(t, m.Enable())
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, "web", lastControlSource.Load())

	m.Disable()
}

func TestCommsManager_StartError(t *testing.T) {
	errBoom := errors.New("boom")

	m := &CommsManager{
		logger:  zerolog.Nop(),
		buildFn: func() *CommsConfig { return &CommsConfig{} },
		startFn: func(_ *CommsConfig) startFunc {
			return func(_ context.Context) error {
				return errBoom
			}
		},
	}

	// Enable should succeed (error is async in goroutine)
	if err := m.Enable(); err != nil {
		t.Fatalf("Enable() error: %v", err)
	}

	// Wait for the goroutine to finish
	time.Sleep(50 * time.Millisecond)

	// The done channel should be closed since start returned
	m.Disable()

	if m.IsRunning() {
		t.Fatal("expected IsRunning() to be false after Disable")
	}
}

// TestCommsManager_EnableRejectsUnknownControlSource verifies that Enable
// runs Validate() before starting the background goroutine, surfacing an
// invalid ControlSource as a synchronous error to the caller.
func TestCommsManager_EnableRejectsUnknownControlSource(t *testing.T) {
	var startCalled atomic.Bool

	m := &CommsManager{
		logger: zerolog.Nop(),
		buildFn: func() *CommsConfig {
			// "bluealsa_xevent" is preserved by normalizeControlSource but
			// not registered in control.Lookup, so Validate must reject it.
			return &CommsConfig{ControlSource: "bluealsa_xevent"}
		},
		startFn: func(_ *CommsConfig) startFunc {
			return func(_ context.Context) error {
				startCalled.Store(true)

				return nil
			}
		},
	}

	err := m.Enable()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown ControlSource")
	assert.False(t, m.IsRunning(), "manager must not be running after Validate failure")
	assert.False(t, startCalled.Load(), "start function must not be invoked when Validate fails")
}

func TestCommsManager_BuildConfigCarriesDSCP(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want int
	}{
		{
			name: "default EF when key absent",
			yaml: "comms:\n  enable: true\n",
			want: config.DefaultCommsDSCP,
		},
		{
			name: "explicit zero survives to comms config",
			yaml: "comms:\n  enable: true\n  dscp: 0\n",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			v.SetConfigType("yaml")
			require.NoError(t, v.ReadConfig(strings.NewReader(tt.yaml)))

			m := NewCommsManager(config.NewWithoutWatch(v), zerolog.Nop(), nil)

			cc := m.buildFn()
			assert.Equal(t, tt.want, cc.DSCP)

			// The operator's `dscp: 0` kill switch must survive
			// applyDefaults: absent-vs-zero is resolved at the config
			// layer only, never re-defaulted here.
			cc.applyDefaults()
			assert.Equal(t, tt.want, cc.DSCP)
		})
	}
}

func TestMixerStartupUpdate_Unconfigured(t *testing.T) {
	v := viper.New()
	cfg := config.NewWithoutWatch(v)

	_, ok := mixerStartupUpdate(cfg)
	assert.False(t, ok, "no comms.audio key set: no startup apply")
}

func TestMixerStartupUpdate_BuildsPartialUpdate(t *testing.T) {
	v := viper.New()
	v.Set("comms.audio.speakerVolume", 80)
	v.Set("comms.audio.agc", false)
	cfg := config.NewWithoutWatch(v)

	u, ok := mixerStartupUpdate(cfg)
	require.True(t, ok)
	require.NotNil(t, u.SpeakerPct)
	assert.Equal(t, 80, *u.SpeakerPct)
	assert.Nil(t, u.MicPct, "unset micVolume must stay nil")
	require.NotNil(t, u.AGC)
	assert.False(t, *u.AGC)
}

// TestBuildCommsConfig_MixerStartupWiring verifies that the closure is
// always wired whenever a hardware mixer is present, even when comms.audio
// is unconfigured at build time — and that invoking it in that state is a
// true no-op that never touches the hardware mixer. The gate now lives
// entirely inside the closure (checked at invocation time), not at build
// time, so a later USB-replug recovery can still pick up config written
// after Enable() (see TestBuildCommsConfig_MixerStartupWiring_PicksUpLateConfig).
func TestBuildCommsConfig_MixerStartupWiring(t *testing.T) {
	var openerCalled bool

	unconfigured := config.NewWithoutWatch(viper.New())
	mixer := &alsa.Volume{
		Log: zerolog.Nop(),
		Open: func(uint) (alsa.Mixer, error) {
			openerCalled = true

			t.Errorf("mixer opener must not be called when comms.audio is unconfigured")

			return nil, errors.New("mixer opener must not be called")
		},
	}
	m := NewCommsManager(unconfigured, zerolog.Nop(), mixer)

	closure := m.buildCommsConfig().AudioMixerStartup
	require.NotNil(t, closure, "closure must always be wired when a mixer is present")

	closure()
	assert.False(t, openerCalled, "unconfigured daemon must never touch the hardware mixer")

	v := viper.New()
	v.Set("comms.audio.speakerVolume", 80)
	configured := config.NewWithoutWatch(v)
	m2 := NewCommsManager(configured, zerolog.Nop(), &alsa.Volume{Log: zerolog.Nop()})
	assert.NotNil(t, m2.buildCommsConfig().AudioMixerStartup, "configured: closure wired")
}

// TestBuildCommsConfig_MixerStartupWiring_PicksUpLateConfig is the
// regression test for the build-time gate bug: a closure built while
// comms.audio was unconfigured must still re-check the config at
// invocation time and attempt the mixer once an operator persists a value
// mid-run (e.g. via the CommsService audio-mixer RPCs, which call
// Config.PersistCommsAudio).
func TestBuildCommsConfig_MixerStartupWiring_PicksUpLateConfig(t *testing.T) {
	t.Setenv("ALSA_CARD", "0")

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("comms:\n  enable: true\n"), 0644))

	v := viper.New()
	v.SetConfigFile(cfgPath)
	require.NoError(t, v.ReadInConfig())

	cfg := config.NewWithoutWatch(v)

	var openerCalled bool

	mixer := &alsa.Volume{
		Log: zerolog.Nop(),
		Open: func(uint) (alsa.Mixer, error) {
			openerCalled = true

			return nil, errors.New("no real card in test")
		},
	}

	m := NewCommsManager(cfg, zerolog.Nop(), mixer)

	// The closure is built once, while comms.audio is still unconfigured.
	closure := m.buildCommsConfig().AudioMixerStartup
	require.NotNil(t, closure)

	closure()
	assert.False(t, openerCalled, "closure built while unconfigured must no-op until comms.audio is set")

	// An operator persists a mixer value mid-run. This is the same
	// v.Set + reload sequence Config.PersistCommsAudio performs when the
	// CommsService handler applies an API-driven volume change.
	speaker := 80
	require.NoError(t, cfg.PersistCommsAudio(&speaker, nil, nil))

	// The SAME closure, built before the config change, must now pick up
	// the update on invocation — the whole point of re-checking config at
	// invocation time instead of gating once at build time.
	closure()
	assert.True(t, openerCalled, "closure must re-check config at invocation time and attempt the mixer once comms.audio is configured")
}
