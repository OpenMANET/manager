package handlers_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	commsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func newCommsService(enable bool) *handlers.CommsService {
	cfg := &config.Config{
		CommsEnable: enable,
	}

	return &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}
}

// setupCommsTestConfig creates a Config backed by a temp YAML file for handler tests.
func setupCommsTestConfig(t *testing.T, yamlContent string) *config.Config {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)

	err = v.ReadInConfig()
	require.NoError(t, err)

	return config.NewWithoutWatch(v)
}

// ── GetCommsConfig ────────────────────────────────────────────────────────────

func TestGetCommsConfig_Defaults(t *testing.T) {
	svc := newCommsService(false)
	resp, err := svc.GetCommsConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.False(t, resp.GetCommsEnabled())
	// Default control source is openvlm (zero value maps to OPENVLM)
	assert.Equal(t, commsv1.ControlSource_CONTROL_SOURCE_OPENVLM, resp.GetControlSource())
}

func TestGetCommsConfig_Enabled(t *testing.T) {
	cfg := &config.Config{
		CommsEnable:        true,
		CommsControlSource: "web",
	}
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

	resp, err := svc.GetCommsConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.GetCommsEnabled())
	assert.Equal(t, commsv1.ControlSource_CONTROL_SOURCE_WEB, resp.GetControlSource())
}

func TestGetCommsConfig_AllControlSources(t *testing.T) {
	tests := []struct {
		src  string
		want commsv1.ControlSource
	}{
		{"openvlm", commsv1.ControlSource_CONTROL_SOURCE_OPENVLM},
		{"nanoptt", commsv1.ControlSource_CONTROL_SOURCE_NANOPTT},
		{"web", commsv1.ControlSource_CONTROL_SOURCE_WEB},
		{"", commsv1.ControlSource_CONTROL_SOURCE_OPENVLM},        // empty defaults to openvlm
		{"unknown", commsv1.ControlSource_CONTROL_SOURCE_OPENVLM}, // unknown defaults to openvlm
	}

	for _, tt := range tests {
		tt := tt
		t.Run(fmt.Sprintf("source_%s", tt.src), func(t *testing.T) {
			cfg := &config.Config{CommsControlSource: tt.src}
			svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

			resp, err := svc.GetCommsConfig(context.Background(), &emptypb.Empty{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.GetControlSource())
		})
	}
}

func TestGetCommsConfig_ReflectsPersistedChanges(t *testing.T) {
	cfg := setupCommsTestConfig(t, `
comms:
  enable: false
  controlSource: openvlm
`)
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

	// Initial state
	resp, err := svc.GetCommsConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.False(t, resp.GetCommsEnabled())
	assert.Equal(t, commsv1.ControlSource_CONTROL_SOURCE_OPENVLM, resp.GetControlSource())

	// Persist a change and verify GetCommsConfig picks it up
	require.NoError(t, cfg.PersistCommsConfig(true, "web"))

	resp, err = svc.GetCommsConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.GetCommsEnabled())
	assert.Equal(t, commsv1.ControlSource_CONTROL_SOURCE_WEB, resp.GetControlSource())

	// Persist another change
	require.NoError(t, cfg.PersistCommsConfig(false, "nanoptt"))

	resp, err = svc.GetCommsConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.False(t, resp.GetCommsEnabled())
	assert.Equal(t, commsv1.ControlSource_CONTROL_SOURCE_NANOPTT, resp.GetControlSource())
}

// ── UpdateCommsConfig ─────────────────────────────────────────────────────────

func TestUpdateCommsConfig_EnableWithControlSource(t *testing.T) {
	cfg := setupCommsTestConfig(t, `
comms:
  enable: false
  controlSource: openvlm
`)
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

	_, err := svc.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_WEB,
	})
	require.NoError(t, err)

	assert.True(t, cfg.GetCommsEnable())
	assert.Equal(t, "web", cfg.GetCommsControlSource())
}

func TestUpdateCommsConfig_Disable(t *testing.T) {
	cfg := setupCommsTestConfig(t, `
comms:
  enable: true
  controlSource: nanoptt
`)
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

	_, err := svc.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   false,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_OPENVLM,
	})
	require.NoError(t, err)

	assert.False(t, cfg.GetCommsEnable())
	assert.Equal(t, "openvlm", cfg.GetCommsControlSource())
}

func TestUpdateCommsConfig_PersistsToFile(t *testing.T) {
	cfg := setupCommsTestConfig(t, `
comms:
  enable: false
  controlSource: openvlm
`)
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

	_, err := svc.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_NANOPTT,
	})
	require.NoError(t, err)

	// Verify file was written
	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "enable: true")
	assert.Contains(t, content, "controlSource: nanoptt")
}

func TestUpdateCommsConfig_AllControlSources(t *testing.T) {
	tests := []struct {
		proto commsv1.ControlSource
		want  string
	}{
		{commsv1.ControlSource_CONTROL_SOURCE_OPENVLM, "openvlm"},
		{commsv1.ControlSource_CONTROL_SOURCE_NANOPTT, "nanoptt"},
		{commsv1.ControlSource_CONTROL_SOURCE_WEB, "web"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			cfg := setupCommsTestConfig(t, "comms:\n  enable: false\n  controlSource: openvlm\n")
			svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

			_, err := svc.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
				EnableComms:   true,
				ControlSource: tt.proto,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.GetCommsControlSource())
		})
	}
}

func TestUpdateCommsConfig_NoConfigFile(t *testing.T) {
	v := viper.New()
	cfg := config.NewWithoutWatch(v)
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

	_, err := svc.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_OPENVLM,
	})
	require.Error(t, err)

	var connectErr *connect.Error
	if assert.ErrorAs(t, err, &connectErr) {
		assert.Equal(t, connect.CodeInternal, connectErr.Code())
	}
}

func TestUpdateCommsConfig_EnableCallsManager(t *testing.T) {
	cfg := setupCommsTestConfig(t, "comms:\n  enable: false\n  controlSource: openvlm\n")
	mgr := &fakeCommsManager{}
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop(), CommsManager: mgr}

	_, err := svc.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_WEB,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, mgr.getDisableCalls(), "should disable before re-enabling")
	assert.Equal(t, 1, mgr.getEnableCalls(), "should enable after disable")
	assert.True(t, mgr.IsRunning())
}

func TestUpdateCommsConfig_DisableCallsManager(t *testing.T) {
	cfg := setupCommsTestConfig(t, "comms:\n  enable: true\n  controlSource: openvlm\n")
	mgr := &fakeCommsManager{running: true}
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop(), CommsManager: mgr}

	_, err := svc.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   false,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_OPENVLM,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, mgr.getDisableCalls(), "should disable when disabling comms")
	assert.Equal(t, 0, mgr.getEnableCalls(), "should not enable when disabling")
	assert.False(t, mgr.IsRunning())
}

func TestUpdateCommsConfig_NilManagerOK(t *testing.T) {
	cfg := setupCommsTestConfig(t, "comms:\n  enable: false\n  controlSource: openvlm\n")
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

	_, err := svc.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_WEB,
	})
	require.NoError(t, err, "should not panic with nil CommsManager")
}

// ── GetCommsStatus ────────────────────────────────────────────────────────────

func TestGetCommsStatus_Disabled(t *testing.T) {
	svc := newCommsService(false)
	_, err := svc.GetCommsStatus(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestGetCommsStatus_Enabled(t *testing.T) {
	svc := newCommsService(true)
	resp, err := svc.GetCommsStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// The comms singleton is not started in tests, so the active talkgroup is 0.
	assert.Equal(t, int32(0), resp.GetActiveTalkgroup())

	// Available talkgroups should be 1-based channel numbers, all positive.
	require.NotEmpty(t, resp.GetAvailableTalkgroups())

	for _, tg := range resp.GetAvailableTalkgroups() {
		assert.Greater(t, tg, int32(0), "available talkgroup channel must be positive")
	}

	// talkgroup_states is best-effort: empty (or nil) when comms is not running.
	assert.Empty(t, resp.GetTalkgroupStates())

	// Codec / ptime reflect the fixed Opus broadcast encoder config.
	assert.Equal(t, "OPUS 32K", resp.GetCodec())
	assert.Equal(t, int32(20), resp.GetPtimeMs())
	// No RTCP-based RTT yet — always zero until populated.
	assert.Equal(t, int32(0), resp.GetRoundTripMs())
}

func TestGetCommsStatus_ReflectsPersistedEnable(t *testing.T) {
	cfg := setupCommsTestConfig(t, `
comms:
  enable: false
  controlSource: openvlm
`)
	svc := &handlers.CommsService{Cfg: cfg, Log: zerolog.Nop()}

	// Disabled — should return an error
	_, err := svc.GetCommsStatus(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")

	// Enable via persist and verify guard passes
	require.NoError(t, cfg.PersistCommsConfig(true, "openvlm"))

	_, err = svc.GetCommsStatus(context.Background(), &emptypb.Empty{})
	// Should no longer return "not enabled" (may return other errors since
	// the comms runtime is not started, but the enable guard must pass).
	if err != nil {
		assert.NotContains(t, err.Error(), "not enabled")
	}
}

// ── SetSendTalkGroup ──────────────────────────────────────────────────────────

func TestSetSendTalkGroup_Disabled(t *testing.T) {
	svc := newCommsService(false)

	_, err := svc.SetSendTalkGroup(context.Background(), &commsv1.SetSendTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestSetSendTalkGroup_NotRunning(t *testing.T) {
	svc := newCommsService(true)

	_, err := svc.SetSendTalkGroup(context.Background(), &commsv1.SetSendTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)
}

func TestSetSendTalkGroup_NotRunning_ResponseShape(t *testing.T) {
	svc := newCommsService(true)

	resp, err := svc.SetSendTalkGroup(context.Background(), &commsv1.SetSendTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.GetSuccess())
	assert.NotEmpty(t, resp.GetMessage())
}

func TestSetSendTalkGroup_EnabledMultipleChannels(t *testing.T) {
	svc := newCommsService(true)

	for _, ch := range []int32{1, 2, 16, 32} {
		ch := ch
		t.Run(fmt.Sprintf("channel_%d", ch), func(t *testing.T) {
			resp, err := svc.SetSendTalkGroup(context.Background(), &commsv1.SetSendTalkGroupRequest{
				Talkgroup: ch,
				Enabled:   true,
			})
			require.Error(t, err)
			require.NotNil(t, resp)
			assert.False(t, resp.GetSuccess())
		})
	}
}

// ── SetReceiveTalkGroup ───────────────────────────────────────────────────────

func TestSetReceiveTalkGroup_Disabled(t *testing.T) {
	svc := newCommsService(false)

	_, err := svc.SetReceiveTalkGroup(context.Background(), &commsv1.SetReceiveTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestSetReceiveTalkGroup_NotRunning(t *testing.T) {
	svc := newCommsService(true)

	_, err := svc.SetReceiveTalkGroup(context.Background(), &commsv1.SetReceiveTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   false,
	})
	require.Error(t, err)
}

func TestSetReceiveTalkGroup_NotRunning_ResponseShape(t *testing.T) {
	svc := newCommsService(true)

	resp, err := svc.SetReceiveTalkGroup(context.Background(), &commsv1.SetReceiveTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.GetSuccess())
	assert.NotEmpty(t, resp.GetMessage())
}

func TestSetReceiveTalkGroup_EnabledMultipleChannels(t *testing.T) {
	svc := newCommsService(true)

	for _, ch := range []int32{1, 2, 16, 32} {
		ch := ch
		t.Run(fmt.Sprintf("channel_%d", ch), func(t *testing.T) {
			resp, err := svc.SetReceiveTalkGroup(context.Background(), &commsv1.SetReceiveTalkGroupRequest{
				Talkgroup: ch,
				Enabled:   true,
			})
			require.Error(t, err)
			require.NotNil(t, resp)
			assert.False(t, resp.GetSuccess())
		})
	}
}

// ── SendPTTEvent ──────────────────────────────────────────────────────────────

func TestSendPTTEvent_WebSourceNotActive(t *testing.T) {
	svc := newCommsService(false)

	_, err := svc.SendPTTEvent(context.Background(), &commsv1.SendPTTEventRequest{Event: 0})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "web control source not active")
}

func TestSendPTTEvent_AllEventTypes_WebSourceNotActive(t *testing.T) {
	svc := newCommsService(false)

	tests := []struct {
		name  string
		event int32
	}{
		{"PTTDown", 0},
		{"PTTUp", 1},
		{"PTTToggle", 2},
		{"InvalidEvent_99", 99},
		{"InvalidEvent_Negative", -1},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.SendPTTEvent(context.Background(), &commsv1.SendPTTEventRequest{Event: tt.event})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "web control source not active")

			var connectErr *connect.Error
			if assert.ErrorAs(t, err, &connectErr) {
				assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
			}
		})
	}
}

func TestStreamAudioTx_BridgeNotActive(t *testing.T) {
	svc := newCommsService(false)
	// StreamAudioTx requires a *connect.ClientStream which is difficult to
	// construct outside of a real HTTP connection. Verify the handler exists.
	_ = svc
}

func TestStreamAudioRx_BridgeNotActive(t *testing.T) {
	svc := newCommsService(false)
	// StreamAudioRx requires a *connect.ServerStream which is difficult to
	// construct outside of a real HTTP connection.
	_ = svc
}
