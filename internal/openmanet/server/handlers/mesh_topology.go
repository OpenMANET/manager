package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/mdlayher/wifi"
	meshtopov1 "github.com/openmanet/openmanetd/internal/api/openmanet/mesh_topology/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MeshTopologyService serves a snapshot of the mesh network topology
// collected by alfred via batadv-vis.
type MeshTopologyService struct {
	Log        zerolog.Logger
	Visibility batmanadv.VisibilityProvider
	Wifi       mgmt.WirelessProvider

	// ParseBatHosts overrides the bat-hosts parser for tests.
	ParseBatHosts func(string) (*batmanadv.BatHosts, error)

	// Now overrides the clock used to stamp collected_at in tests.
	Now func() time.Time
}

func (s *MeshTopologyService) parseBatHosts(path string) (*batmanadv.BatHosts, error) {
	if s.ParseBatHosts != nil {
		return s.ParseBatHosts(path)
	}

	return batmanadv.ParseBatHostsFile(path)
}

func (s *MeshTopologyService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// GetMeshTopology returns the current mesh topology snapshot.
func (s *MeshTopologyService) GetMeshTopology(_ context.Context, _ *emptypb.Empty) (*meshtopov1.GetMeshTopologyResponse, error) {
	s.Log.Debug().Msg("GetMeshTopology Request Received")

	batHosts, err := s.parseBatHosts(batmanadv.BatHostsFilePath)
	if err != nil {
		s.Log.Warn().Err(err).Msg("Failed to parse bat-hosts; hostname enrichment disabled")

		batHosts = &batmanadv.BatHosts{}
	}

	doc, err := s.Visibility.GetVisibility()
	if err != nil {
		if errors.Is(err, batmanadv.ErrVisUnavailable) {
			s.Log.Warn().Err(err).Msg("batadv-vis unavailable")

			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("batadv-vis unavailable: %w", err))
		}

		s.Log.Error().Err(err).Msg("Failed to get mesh visibility")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mesh visibility: %w", err))
	}

	localIfaces, stations := s.buildLocalStationLookup()

	topology := &meshtopov1.MeshTopology{
		SourceVersion: doc.SourceVersion,
		Algorithm:     int32(doc.Algorithm),
		CollectedAt:   timestamppb.New(s.now()),
		Nodes:         make([]*meshtopov1.MeshNode, 0, len(doc.Vis)),
	}

	for _, entry := range doc.Vis {
		node := &meshtopov1.MeshNode{
			PrimaryMac:      entry.Primary,
			PrimaryHostname: batHosts.GetHostByMAC(entry.Primary),
			SecondaryMacs:   entry.Secondary,
			Neighbors:       make([]*meshtopov1.MeshEdge, 0, len(entry.Neighbors)),
			Clients:         make([]*meshtopov1.MeshClient, 0, len(entry.Clients)),
		}

		for _, n := range entry.Neighbors {
			edge := &meshtopov1.MeshEdge{
				RouterMac:        n.Router,
				NeighborMac:      n.Neighbor,
				NeighborHostname: batHosts.GetHostByMAC(n.Neighbor),
				Metric:           batmanadv.ParseMetric(n.Metric),
			}

			// Signal is only meaningful when the edge originates from one of
			// our own radios — we can only measure our own outgoing links. If
			// router_mac belongs to another node, we do not have signal data
			// for that edge.
			if _, isLocal := localIfaces[strings.ToLower(n.Router)]; isLocal {
				if station, ok := stations[strings.ToLower(n.Neighbor)]; ok {
					edge.Signal = int32(station.Signal)
					edge.SignalAverage = int32(station.SignalAverage)
				}
			}

			node.Neighbors = append(node.Neighbors, edge)
		}

		for _, mac := range entry.Clients {
			node.Clients = append(node.Clients, &meshtopov1.MeshClient{
				Mac:      mac,
				Hostname: batHosts.GetHostByMAC(mac),
			})
		}

		topology.Nodes = append(topology.Nodes, node)
	}

	return &meshtopov1.GetMeshTopologyResponse{Topology: topology}, nil
}

// buildLocalStationLookup returns the set of this node's mesh interface MAC
// addresses and a MAC -> StationInfo map for the remote peers visible on
// those interfaces. Failures are logged and yield empty maps so the topology
// response still succeeds.
func (s *MeshTopologyService) buildLocalStationLookup() (map[string]struct{}, map[string]*wifi.StationInfo) {
	localIfaces := make(map[string]struct{})
	stations := make(map[string]*wifi.StationInfo)

	if s.Wifi == nil {
		return localIfaces, stations
	}

	ifaces, err := s.Wifi.GetMeshInterfaces()
	if err != nil {
		s.Log.Warn().Err(err).Msg("Failed to list mesh interfaces; signal enrichment disabled")

		return localIfaces, stations
	}

	for _, iface := range ifaces {
		if iface == nil {
			continue
		}

		localIfaces[strings.ToLower(iface.HardwareAddr.String())] = struct{}{}

		ifaceStations, err := s.Wifi.StationInfo(iface)
		if err != nil {
			s.Log.Warn().Err(err).Str("iface", iface.Name).Msg("Failed to read station info; signal enrichment partial")

			continue
		}

		for _, station := range ifaceStations {
			if station == nil {
				continue
			}

			stations[strings.ToLower(station.HardwareAddr.String())] = station
		}
	}

	return localIfaces, stations
}
