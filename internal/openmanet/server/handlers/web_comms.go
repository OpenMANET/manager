package handlers

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/comms"
	"github.com/rs/zerolog"
)

// WebCommsService implements the WebCommsServiceHandler interface generated
// by connect-go. It bridges web-based PTT and audio streaming to the comms
// subsystem.
type WebCommsService struct {
	Log zerolog.Logger
}

// SendPTTEvent delivers a PTT state change from the web client to the comms
// event loop. Returns FailedPrecondition when the web control source is not
// active.
func (s *WebCommsService) SendPTTEvent(_ context.Context, req *serviceproto.SendPTTEventRequest) (*serviceproto.SendPTTEventResponse, error) {
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

	return &serviceproto.SendPTTEventResponse{Success: true}, nil
}

// StreamAudioTx receives a stream of Opus-encoded audio frames from the web
// client and injects them into the RTP send path. Returns FailedPrecondition
// when the web audio bridge is not active.
func (s *WebCommsService) StreamAudioTx(_ context.Context, stream *connect.ClientStream[serviceproto.WebAudioFrame]) (*serviceproto.StreamAudioTxResponse, error) {
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

	return &serviceproto.StreamAudioTxResponse{FramesReceived: count}, nil
}

// StreamAudioRx streams Opus-encoded audio frames received from the mesh
// back to the web client. Returns FailedPrecondition when the web audio
// bridge is not active.
func (s *WebCommsService) StreamAudioRx(ctx context.Context, _ *serviceproto.StreamAudioRxRequest, stream *connect.ServerStream[serviceproto.WebAudioFrame]) error {
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

			if err := stream.Send(&serviceproto.WebAudioFrame{
				OpusData: opusData,
				Sequence: seq,
			}); err != nil {
				return err
			}
		}
	}
}
