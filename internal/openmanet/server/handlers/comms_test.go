package handlers_test

import (
	"context"
	"fmt"
	"testing"

	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func newCommsService(enable bool) *handlers.CommsService {
	cfg := &config.Config{
		CommsEnable: enable,
	}

	return &handlers.CommsService{Cfg: cfg}
}

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
}

func TestSetSendTalkGroup_Disabled(t *testing.T) {
	svc := newCommsService(false)

	_, err := svc.SetSendTalkGroup(context.Background(), &serviceproto.SetSendTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestSetSendTalkGroup_NotRunning(t *testing.T) {
	// comms enabled but singleton never started → GetTalkGroupStates returns an error.
	svc := newCommsService(true)

	_, err := svc.SetSendTalkGroup(context.Background(), &serviceproto.SetSendTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)
}

func TestSetSendTalkGroup_NotRunning_ResponseShape(t *testing.T) {
	svc := newCommsService(true)

	resp, err := svc.SetSendTalkGroup(context.Background(), &serviceproto.SetSendTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)
	// When comms is enabled but the runtime is not started, the handler returns
	// a non-nil response with Success=false and a descriptive Message.
	require.NotNil(t, resp)
	assert.False(t, resp.GetSuccess())
	assert.NotEmpty(t, resp.GetMessage())
}

func TestSetSendTalkGroup_EnabledMultipleChannels(t *testing.T) {
	svc := newCommsService(true)

	// Channels within valid range all fail the same way when the runtime
	// is not started — verify a few representative values.
	for _, ch := range []int32{1, 2, 16, 32} {
		ch := ch
		t.Run(fmt.Sprintf("channel_%d", ch), func(t *testing.T) {
			resp, err := svc.SetSendTalkGroup(context.Background(), &serviceproto.SetSendTalkGroupRequest{
				Talkgroup: ch,
				Enabled:   true,
			})
			require.Error(t, err)
			require.NotNil(t, resp)
			assert.False(t, resp.GetSuccess())
		})
	}
}

func TestSetReceiveTalkGroup_Disabled(t *testing.T) {
	svc := newCommsService(false)

	_, err := svc.SetReceiveTalkGroup(context.Background(), &serviceproto.SetReceiveTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestSetReceiveTalkGroup_NotRunning(t *testing.T) {
	// comms enabled but singleton never started → GetTalkGroupStates returns an error.
	svc := newCommsService(true)

	_, err := svc.SetReceiveTalkGroup(context.Background(), &serviceproto.SetReceiveTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   false,
	})
	require.Error(t, err)
}

func TestSetReceiveTalkGroup_NotRunning_ResponseShape(t *testing.T) {
	svc := newCommsService(true)

	resp, err := svc.SetReceiveTalkGroup(context.Background(), &serviceproto.SetReceiveTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)
	// When comms is enabled but the runtime is not started, the handler returns
	// a non-nil response with Success=false and a descriptive Message.
	require.NotNil(t, resp)
	assert.False(t, resp.GetSuccess())
	assert.NotEmpty(t, resp.GetMessage())
}

func TestSetReceiveTalkGroup_EnabledMultipleChannels(t *testing.T) {
	svc := newCommsService(true)

	for _, ch := range []int32{1, 2, 16, 32} {
		ch := ch
		t.Run(fmt.Sprintf("channel_%d", ch), func(t *testing.T) {
			resp, err := svc.SetReceiveTalkGroup(context.Background(), &serviceproto.SetReceiveTalkGroupRequest{
				Talkgroup: ch,
				Enabled:   true,
			})
			require.Error(t, err)
			require.NotNil(t, resp)
			assert.False(t, resp.GetSuccess())
		})
	}
}
