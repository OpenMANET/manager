package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	meshtopov1 "github.com/openmanet/openmanetd/internal/api/openmanet/mesh_topology/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MeshTopologyService serves a snapshot of the local batman-adv originator
// table enriched with /tmp/bat-hosts friendly names.
type MeshTopologyService struct {
	Log zerolog.Logger

	// OrigProvider produces the enriched originator snapshot. Wired to
	// BatctlOriginatorTopologyProvider in production; tests swap in a
	// hand-rolled fake.
	OrigProvider batmanadv.OriginatorTopologyProvider

	// DeltaTracker supplies rolling churn metrics for
	// GetMeshTopologyDelta. When nil, the RPC returns
	// CodeFailedPrecondition.
	DeltaTracker *DeltaTracker

	// Now overrides the clock used to stamp collected_at in tests.
	Now func() time.Time
}

func (s *MeshTopologyService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// GetMeshTopology returns the current originator-based topology snapshot.
func (s *MeshTopologyService) GetMeshTopology(_ context.Context, _ *emptypb.Empty) (*meshtopov1.GetMeshTopologyResponse, error) {
	s.Log.Debug().Msg("GetMeshTopology Request Received")

	snap, err := s.OrigProvider.GetOriginatorTopology()
	if err != nil {
		if errors.Is(err, batmanadv.ErrOriginatorsUnavailable) {
			s.Log.Warn().Err(err).Msg("batctl originators unavailable")

			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("originators unavailable: %w", err))
		}

		s.Log.Error().Err(err).Msg("Failed to get originator topology")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("originator topology: %w", err))
	}

	out := &meshtopov1.MeshTopology{
		SelfMac:      snap.SelfMAC,
		SelfHostname: snap.SelfHostname,
		Algorithm:    snap.Algorithm,
		CollectedAt:  timestamppb.New(s.now()),
		Originators:  make([]*meshtopov1.MeshOriginator, 0, len(snap.Originators)),
	}

	for _, o := range snap.Originators {
		out.Originators = append(out.Originators, &meshtopov1.MeshOriginator{
			OrigMac:         o.OrigMAC,
			OrigHostname:    o.OrigHostname,
			NextHopMac:      o.NextHopMAC,
			NextHopHostname: o.NextHopHostname,
			HardIfname:      o.HardIfname,
			Tq:              int32(o.TQ),
			Throughput:      o.Throughput,
			LastSeenMs:      int32(o.LastSeenMs),
			Hops:            int32(o.Hops),
		})
	}

	return &meshtopov1.GetMeshTopologyResponse{Topology: out}, nil
}

// GetMeshTopologyDelta returns the aggregated churn metrics over the
// requested look-back window.
func (s *MeshTopologyService) GetMeshTopologyDelta(_ context.Context, req *meshtopov1.GetMeshTopologyDeltaRequest) (*meshtopov1.GetMeshTopologyDeltaResponse, error) {
	if s.DeltaTracker == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("mesh topology delta tracker is not running"))
	}

	var window time.Duration

	if req != nil && req.Window != nil {
		window = req.Window.AsDuration()
	}

	result := s.DeltaTracker.Window(window)

	return &meshtopov1.GetMeshTopologyDeltaResponse{
		RoutesAdded:    result.RoutesAdded,
		RoutesLost:     result.RoutesLost,
		GatewayChanges: result.GatewayChanges,
		Reconverge:     durationpb.New(result.Reconverge),
		ActualWindow:   durationpb.New(result.ActualWindow),
	}, nil
}
