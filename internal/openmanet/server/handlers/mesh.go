package handlers

import (
	"context"
	"fmt"

	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

type MeshService struct {
	Log           zerolog.Logger
	Wifi          mgmt.WirelessProvider
	ParseBatHosts func(string) (*batmanadv.BatHosts, error)
}

func (m *MeshService) parseBatHosts(path string) (*batmanadv.BatHosts, error) {
	if m.ParseBatHosts != nil {
		return m.ParseBatHosts(path)
	}

	return batmanadv.ParseBatHostsFile(path)
}

func (m *MeshService) ListMeshNeighbors(_ context.Context, _ *emptypb.Empty) (*serviceproto.ListMeshNeighborsResponse, error) {
	var (
		protoNeighbors []*serviceproto.MeshNeighbor
	)

	m.Log.Debug().Msg("ListMeshNeighbors Request Received")

	// Get batman-adv hosts file
	batHosts, err := m.parseBatHosts(batmanadv.BatHostsFilePath)
	if err != nil {
		m.Log.Error().Err(err).Msg("Failed to parse batman-adv hosts file")

		return nil, err
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

			return nil, fmt.Errorf("station info for %s: %w", meshInterface.Name, err)
		}

		// Map connected stations to batman-adv hosts to get hostnames
		// TODO: Get last seen from batman-adv neighbors and change throughput to batman-adv metric
		for _, station := range connectedStations {
			protoNeighbors = append(protoNeighbors, &serviceproto.MeshNeighbor{
				Neighbor:        batHosts.GetHostByMAC(station.HardwareAddr.String()),
				HardwareAddress: station.HardwareAddr.String(),
				SignalStrength:  int32(station.SignalAverage),
				Signal:          int32(station.Signal),
				Throughput:      int32(station.TransmitBitrate),
			})
		}
	}

	return &serviceproto.ListMeshNeighborsResponse{
		Neighbors: protoNeighbors,
	}, nil
}
