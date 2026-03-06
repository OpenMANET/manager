package handlers

import (
	"context"
	"errors"

	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

type CommsService struct {
	Cfg *config.Config
	Log zerolog.Logger
}

// GetCommsStatus retrieves the current status of the communications service.
// It returns the active talk group and a list of all available talk groups.
//
// The active talk group is determined by the current multicast address in use,
// while the available talk groups are derived from the configured multicast
// talk group addresses. All talk groups use port 5007.
//
// Parameters:
//   - _: context.Context - unused context parameter
//   - _: *emptypb.Empty - unused empty request parameter
//
// Returns:
//   - *serviceproto.GetCommsStatusResponse: Contains the active talk group and
//     list of available talk groups
//   - error: Returns nil on success, or an error if the status cannot be retrieved
func (c *CommsService) GetCommsStatus(_ context.Context, _ *emptypb.Empty) (*serviceproto.GetCommsStatusResponse, error) {
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

	return &serviceproto.GetCommsStatusResponse{
		ActiveTalkgroup:     activeTalkGroupChannel,
		AvailableTalkgroups: talkGroupProtos,
		TalkgroupStates:     buildTalkGroupStates(),
	}, nil
}

// buildTalkGroupStates returns a proto slice of TalkGroupState by querying the
// comms runtime. It returns nil (not an error) when comms is not running so
// that GetCommsStatus remains usable even when the subsystem is disabled.
func buildTalkGroupStates() []*serviceproto.TalkGroupState {
	states, err := comms.GetTalkGroupStates()
	if err != nil {
		return nil
	}

	result := make([]*serviceproto.TalkGroupState, 0, len(states))

	for _, s := range states {
		ch, chErr := config.TalkGroupChannel(s.Port)
		if chErr != nil {
			continue // skip ports that don't map to a channel
		}

		result = append(result, &serviceproto.TalkGroupState{
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
func (c *CommsService) SetSendTalkGroup(_ context.Context, req *serviceproto.SetSendTalkGroupRequest) (*serviceproto.SetSendTalkGroupResponse, error) {
	if !c.Cfg.CommsEnable {
		return nil, errors.New("comms module not enabled")
	}

	portIdx, err := talkGroupPortIdx(int(req.GetTalkgroup()))
	if err != nil {
		return &serviceproto.SetSendTalkGroupResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}

	if err := comms.EnableTalkGroupSend(portIdx, req.GetEnabled()); err != nil {
		return &serviceproto.SetSendTalkGroupResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}

	return &serviceproto.SetSendTalkGroupResponse{
		Success: true,
	}, nil
}

// SetReceiveTalkGroup enables or disables RTP reception on the talkgroup
// identified by the 1-based channel number in the request.
func (c *CommsService) SetReceiveTalkGroup(_ context.Context, req *serviceproto.SetReceiveTalkGroupRequest) (*serviceproto.SetReceiveTalkGroupResponse, error) {
	if !c.Cfg.CommsEnable {
		return nil, errors.New("comms module not enabled")
	}

	portIdx, err := talkGroupPortIdx(int(req.GetTalkgroup()))
	if err != nil {
		return &serviceproto.SetReceiveTalkGroupResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}

	if err := comms.EnableTalkGroupReceive(portIdx, req.GetEnabled()); err != nil {
		return &serviceproto.SetReceiveTalkGroupResponse{
			Success: false,
			Message: err.Error(),
		}, err
	}

	return &serviceproto.SetReceiveTalkGroupResponse{
		Success: true,
	}, nil
}
