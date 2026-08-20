package handlers_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	commsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1"
	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/openmanet/openmanetd/internal/comms/talkgroup"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestSelectTalkGroup_CommsDisabled(t *testing.T) {
	svc := &handlers.CommsService{
		Log:     zerolog.Nop(),
		Cfg:     setupCommsTestConfig(t, "comms:\n  enable: false\n"),
		Service: func() *comms.Service { return nil },
	}

	_, err := svc.SelectTalkGroup(context.Background(),
		&commsv1.SelectTalkGroupRequest{Talkgroup: 2})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
}

func TestSelectTalkGroup_NotRunning(t *testing.T) {
	svc := &handlers.CommsService{
		Log:     zerolog.Nop(),
		Cfg:     setupCommsTestConfig(t, "comms:\n  enable: true\n"),
		Service: func() *comms.Service { return nil }, // comms enabled but not running
	}

	_, err := svc.SelectTalkGroup(context.Background(),
		&commsv1.SelectTalkGroupRequest{Talkgroup: 2})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
}

func TestTalkGroupEventToProto(t *testing.T) {
	at := time.Now()
	got := handlers.TalkGroupEventToProto(talkgroup.Event{
		Kind: talkgroup.KindSelected, Channel: 3, Prev: 1,
		Send: true, Receive: true, Source: talkgroup.SourceGPIO, At: at,
	})

	assert.Equal(t, commsv1.TalkGroupEventKind_TALK_GROUP_EVENT_KIND_SELECTED, got.Kind)
	assert.Equal(t, int32(3), got.Talkgroup)
	assert.Equal(t, int32(1), got.PrevTalkgroup)
	assert.True(t, got.SendEnabled)
	assert.True(t, got.ReceiveEnabled)
	assert.Equal(t, commsv1.TalkGroupEventSource_TALK_GROUP_EVENT_SOURCE_GPIO, got.Source)
	assert.Equal(t, at.Unix(), got.At.AsTime().Unix())
}

func TestStreamTalkGroupEvents_NotRunning(t *testing.T) {
	svc := &handlers.CommsService{
		Log:     zerolog.Nop(),
		Cfg:     setupCommsTestConfig(t, "comms:\n  enable: true\n"),
		Service: func() *comms.Service { return nil },
	}

	err := svc.StreamTalkGroupEvents(context.Background(), &emptypb.Empty{}, nil)
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
}
