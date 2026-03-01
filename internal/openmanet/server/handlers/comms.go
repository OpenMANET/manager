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
	}, nil
}

// JoinTalkGroup handles requests to join a multicast talk group by updating the multicast endpoint.
// It first checks if the Comms module is enabled in the configuration, returning an error if not.
// If enabled, it attempts to update the multicast endpoint using the provided address and the default
// comms port. Returns a JoinTalkGroupResponse with Success set to true if the operation succeeds,
// or an error if the Comms module is disabled or if updating the multicast endpoint fails.
func (c *CommsService) JoinTalkGroup(_ context.Context, req *serviceproto.JoinTalkGroupRequest) (*serviceproto.JoinTalkGroupResponse, error) {
	if !c.Cfg.CommsEnable {
		return nil, errors.New("comms module not enabled")
	}

	talkgroup := int(req.GetTalkgroup())
	if talkgroup == 0 {
		talkgroup = 1 // default to channel 1
	}

	mCastPort, err := config.TalkGroupPort(talkgroup)
	if err != nil {
		return nil, err
	}

	err = comms.UpdateMulticastEndpoint(config.TalkGroupMcastAddr, mCastPort)
	if err != nil {
		return nil, err
	}

	return &serviceproto.JoinTalkGroupResponse{
		Success: true,
	}, nil
}
