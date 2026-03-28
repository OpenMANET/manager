package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	commsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1"
	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

// CommsService implements the commsv1connect.CommsServiceHandler interface,
// combining configuration, status, talkgroup control, PTT events, and audio
// streaming into a single handler.
type CommsService struct {
	Cfg          *config.Config
	Log          zerolog.Logger
	CommsManager comms.CommsLifecycle
}

// controlSourceToProto maps a config string to the proto ControlSource enum.
func controlSourceToProto(src string) commsv1.ControlSource {
	switch strings.ToLower(src) {
	case "nanoptt":
		return commsv1.ControlSource_CONTROL_SOURCE_NANOPTT
	case "web":
		return commsv1.ControlSource_CONTROL_SOURCE_WEB
	default:
		return commsv1.ControlSource_CONTROL_SOURCE_CM108
	}
}

// protoToControlSource maps a proto ControlSource enum to the config string.
func protoToControlSource(src commsv1.ControlSource) string {
	switch src {
	case commsv1.ControlSource_CONTROL_SOURCE_NANOPTT:
		return "nanoptt"
	case commsv1.ControlSource_CONTROL_SOURCE_WEB:
		return "web"
	default:
		return "cm108"
	}
}

// GetCommsConfig retrieves the current communications configuration.
func (c *CommsService) GetCommsConfig(_ context.Context, _ *emptypb.Empty) (*commsv1.GetCommsConfigResponse, error) {
	return &commsv1.GetCommsConfigResponse{
		CommsEnabled:  c.Cfg.CommsEnable,
		ControlSource: controlSourceToProto(c.Cfg.CommsControlSource),
	}, nil
}

// UpdateCommsConfig updates the EnableComms and ControlSource settings,
// persisting them to the config file and applying changes at runtime.
func (c *CommsService) UpdateCommsConfig(_ context.Context, req *commsv1.UpdateCommsConfigRequest) (*commsv1.UpdateCommsConfigResponse, error) {
	ctrlSrc := protoToControlSource(req.GetControlSource())

	if err := c.Cfg.PersistCommsConfig(req.GetEnableComms(), ctrlSrc); err != nil {
		c.Log.Error().Err(err).Msg("Failed to persist comms config")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to persist comms config: %w", err))
	}

	// Apply runtime changes via the CommsManager.
	if c.CommsManager != nil {
		// Always stop current instance first (handles control source swap).
		c.CommsManager.Disable()

		if req.GetEnableComms() {
			if err := c.CommsManager.Enable(); err != nil {
				c.Log.Error().Err(err).Msg("Failed to enable comms at runtime")

				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to enable comms: %w", err))
			}
		}
	}

	return &commsv1.UpdateCommsConfigResponse{}, nil
}

// GetCommsStatus retrieves the current status of the communications service.
// It returns the active talk group and a list of all available talk groups.
func (c *CommsService) GetCommsStatus(_ context.Context, _ *emptypb.Empty) (*commsv1.GetCommsStatusResponse, error) {
	if !c.Cfg.CommsEnable {
		return nil, errors.New("comms module not enabled")
	}

	availableTalkGroups := config.GetMulticastTalkGroups()

	talkGroupProtos := make([]int32, len(availableTalkGroups))
	for i, tg := range availableTalkGroups {
		channel, err := config.TalkGroupChannel(tg.Port)
		if err != nil {
			return nil, err
		}

		talkGroupProtos[i] = int32(channel)
	}

	activeTalkGroupPort := comms.GetActiveMulticastPort()

	var activeTalkGroupChannel int32

	if activeTalkGroupPort != 0 {
		ch, err := config.TalkGroupChannel(activeTalkGroupPort)
		if err != nil {
			return nil, err
		}

		activeTalkGroupChannel = int32(ch)
	}

	return &commsv1.GetCommsStatusResponse{
		ActiveTalkgroup:     activeTalkGroupChannel,
		AvailableTalkgroups: talkGroupProtos,
		TalkgroupStates:     buildTalkGroupStates(),
	}, nil
}

// buildTalkGroupStates returns a proto slice of TalkGroupState by querying the
// comms runtime. It returns nil (not an error) when comms is not running so
// that GetCommsStatus remains usable even when the subsystem is disabled.
func buildTalkGroupStates() []*commsv1.TalkGroupState {
	states, err := comms.GetTalkGroupStates()
	if err != nil {
		return nil
	}

	result := make([]*commsv1.TalkGroupState, 0, len(states))

	for _, s := range states {
		ch, chErr := config.TalkGroupChannel(s.Port)
		if chErr != nil {
			continue // skip ports that don't map to a channel
		}

		result = append(result, &commsv1.TalkGroupState{
			Talkgroup:      int32(ch),
			Address:        s.Address,
			Port:           int32(s.Port),
			SendEnabled:    s.SendEnabled,
			ReceiveEnabled: s.ReceiveEnabled,
		})
	}

	return result
}

// talkGroupPortIdx resolves a 1-based channel number to the zero-based port
// index used by comms.EnableTalkGroupSend / comms.EnableTalkGroupReceive.
func talkGroupPortIdx(channel int) (int, error) {
	targetPort, err := config.TalkGroupPort(channel)
	if err != nil {
		return 0, err
	}

	states, err := comms.GetTalkGroupStates()
	if err != nil {
		return 0, err
	}

	for i, s := range states {
		if s.Port == targetPort {
			return i, nil
		}
	}

	return 0, errors.New("talkgroup channel not found in active comms configuration")
}

// SetSendTalkGroup enables or disables RTP transmission on the talkgroup
// identified by the 1-based channel number in the request.
func (c *CommsService) SetSendTalkGroup(_ context.Context, req *commsv1.SetSendTalkGroupRequest) (*commsv1.SetSendTalkGroupResponse, error) {
	if !c.Cfg.CommsEnable {
		return nil, errors.New("comms module not enabled")
	}

	portIdx, err := talkGroupPortIdx(int(req.GetTalkgroup()))
	if err != nil {
		return &commsv1.SetSendTalkGroupResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}

	if err := comms.EnableTalkGroupSend(portIdx, req.GetEnabled()); err != nil {
		return &commsv1.SetSendTalkGroupResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}

	return &commsv1.SetSendTalkGroupResponse{
		Success: true,
	}, nil
}

// SetReceiveTalkGroup enables or disables RTP reception on the talkgroup
// identified by the 1-based channel number in the request.
func (c *CommsService) SetReceiveTalkGroup(_ context.Context, req *commsv1.SetReceiveTalkGroupRequest) (*commsv1.SetReceiveTalkGroupResponse, error) {
	if !c.Cfg.CommsEnable {
		return nil, errors.New("comms module not enabled")
	}

	portIdx, err := talkGroupPortIdx(int(req.GetTalkgroup()))
	if err != nil {
		return &commsv1.SetReceiveTalkGroupResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}

	if err := comms.EnableTalkGroupReceive(portIdx, req.GetEnabled()); err != nil {
		return &commsv1.SetReceiveTalkGroupResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}

	return &commsv1.SetReceiveTalkGroupResponse{
		Success: true,
	}, nil
}

// SendPTTEvent delivers a PTT state change from the web client to the comms
// event loop. Returns FailedPrecondition when the web control source is not
// active.
func (c *CommsService) SendPTTEvent(_ context.Context, req *commsv1.SendPTTEventRequest) (*commsv1.SendPTTEventResponse, error) {
	ws := comms.GetWebEventSource()
	if ws == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("web control source not active"))
	}

	var ev comms.PTTEvent

	switch req.GetEvent() {
	case 0:
		ev = comms.PTTDown
	case 1:
		ev = comms.PTTUp
	case 2:
		ev = comms.PTTToggle
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid PTT event type"))
	}

	ws.Push(ev)

	return &commsv1.SendPTTEventResponse{Success: true}, nil
}

// StreamAudioTx receives a stream of Opus-encoded audio frames from the web
// client and injects them into the RTP send path. Returns FailedPrecondition
// when the web audio bridge is not active.
func (c *CommsService) StreamAudioTx(_ context.Context, stream *connect.ClientStream[commsv1.WebAudioFrame]) (*commsv1.StreamAudioTxResponse, error) {
	bridge := comms.GetWebAudioBridge()
	if bridge == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("web audio bridge not active"))
	}

	var count uint32

	for stream.Receive() {
		msg := stream.Msg()
		bridge.InjectTxFrame(msg.GetOpusData())

		count++
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	c.Log.Debug().Uint32("frames_received", count).Msg("Received audio frames from web client")

	return &commsv1.StreamAudioTxResponse{FramesReceived: count}, nil
}

// StreamAudioRx streams Opus-encoded audio frames received from the mesh
// back to the web client. Returns FailedPrecondition when the web audio
// bridge is not active.
func (c *CommsService) StreamAudioRx(ctx context.Context, _ *commsv1.StreamAudioRxRequest, stream *connect.ServerStream[commsv1.WebAudioFrame]) error {
	bridge := comms.GetWebAudioBridge()
	if bridge == nil {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("web audio bridge not active"))
	}

	var seq uint32

	for {
		select {
		case <-ctx.Done():
			return nil
		case opusData, ok := <-bridge.RxFrames():
			if !ok {
				return nil
			}

			seq++

			if err := stream.Send(&commsv1.WebAudioFrame{
				OpusData: opusData,
				Sequence: seq,
			}); err != nil {
				return err
			}

			c.Log.Debug().Uint32("seq", seq).Msg("Sent audio frame to web client")
		}
	}
}
