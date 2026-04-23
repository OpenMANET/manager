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
	Log              zerolog.Logger
	Wifi             mgmt.WirelessProvider
	ParseBatHosts    func(string) (*batmanadv.BatHosts, error)
	GetMeshNeighbors func() (*batmanadv.Neighbors, error)
}

func (m *MeshService) parseBatHosts(path string) (*batmanadv.BatHosts, error) {
	if m.ParseBatHosts != nil {
		return m.ParseBatHosts(path)
	}

	return batmanadv.ParseBatHostsFile(path)
}

func (m *MeshService) getMeshNeighbors() (*batmanadv.Neighbors, error) {
	if m.GetMeshNeighbors != nil {
		return m.GetMeshNeighbors()
	}

	return batmanadv.GetMeshNeighbors()
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

	// Get batman-adv neighbors for last_seen and throughput
	batNeighbors, err := m.getMeshNeighbors()
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

			return nil, fmt.Errorf("station info for %s: %w", meshInterface.Name, err)
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
