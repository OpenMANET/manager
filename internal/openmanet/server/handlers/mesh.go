package handlers

import (
	"context"

	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

type MeshService struct {
	Log  zerolog.Logger
	Wifi *mgmt.WirelessConfig
}

func (m *MeshService) ListMeshNeighbors(_ context.Context, _ *emptypb.Empty) (*serviceproto.ListMeshNeighborsResponse, error) {
	var (
		protoNeighbors []*serviceproto.MeshNeighbor
	)
	m.Log.Debug().Msg("ListMeshNeighbors Request Received")

	// Get batman-adv hosts file
	batHosts, err := batmanadv.ParseBatHostsFile(batmanadv.BatHostsFilePath)
	if err != nil {
		m.Log.Error().Err(err).Msg("Failed to parse batman-adv hosts file")
		return nil, err
	}

	// Get batman-adv neighbors for last_seen and throughput
	batNeighbors, err := batmanadv.GetMeshNeighbors()
	if err != nil {
		m.Log.Warn().Err(err).Msg("Failed to get batman-adv neighbors, last_seen and throughput will be unavailable")

		batNeighbors = nil
	}

	// Get mesh wifi interfaces
	meshInterfaces, err := m.Wifi.GetMeshInterfaces()
	if err != nil {
		m.Log.Error().Err(err).Msg("Failed to list mesh neighbors")
		return nil, err
	}

	for _, meshInterface := range meshInterfaces {
		// Get the wifi stations connected to the mesh interface
		connectedStations, err := m.Wifi.StationInfo(meshInterface)
		if err != nil {
			m.Log.Error().Err(err).Msgf("Failed to get station info for interface: %s", meshInterface.Name)
			return nil, err
		}

		// Map connected stations to batman-adv hosts to get hostnames
		for _, station := range connectedStations {
			neighbor := &serviceproto.MeshNeighbor{
				Neighbor:        batHosts.GetHostByMAC(station.HardwareAddr.String()),
				HardwareAddress: station.HardwareAddr.String(),
				SignalStrength:  int32(station.SignalAverage),
				Signal:          int32(station.Signal),
				Throughput:      int32(station.TransmitBitrate),
			}

			// Enrich with batman-adv neighbor data if available
			if batNeighbor := batNeighbors.FindByNeighAddress(station.HardwareAddr.String()); batNeighbor != nil {
				neighbor.LastSeen = int64(batNeighbor.LastSeenMsecs)
				neighbor.Throughput = int32(batNeighbor.Throughput)
			}

			protoNeighbors = append(protoNeighbors, neighbor)
		}
	}

	return &serviceproto.ListMeshNeighborsResponse{
		Neighbors: protoNeighbors,
	}, nil
}
