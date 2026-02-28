package handlers

import (
	"context"
	"fmt"

	"github.com/mdlayher/wifi"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

type StatusService struct {
	Cfg  *config.Config
	Log  zerolog.Logger
	Wifi *mgmt.WirelessConfig
	GPS  *gpsd.GPSService
}

func (s *StatusService) GetServiceStatus(_ context.Context, _ *emptypb.Empty) (*serviceproto.GetServiceStatusResponse, error) {
	var (
		meshConnected      bool
		isMeshGateway      bool
		connectedNeighbors int32
		numMeshInterfaces  int32
	)

	s.Log.Debug().Msg("GetStatus Request Received")

	// Get mesh wifi interfaces
	meshInterfaces, err := s.Wifi.GetMeshInterfaces()
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to list mesh neighbors")

		return nil, err
	}

	for _, meshInterface := range meshInterfaces {
		// Get the wifi stations connected to the mesh interface
		var connectedStations []*wifi.StationInfo

		connectedStations, err = s.Wifi.StationInfo(meshInterface)
		if err != nil {
			s.Log.Error().Err(err).Msgf("Failed to get station info for interface: %s", meshInterface.Name)

			return nil, fmt.Errorf("station info for %s: %w", meshInterface.Name, err)
		}

		if len(connectedStations) > 0 {
			connectedNeighbors += int32(len(connectedStations))
			meshConnected = true

			break
		}
	}

	numMeshInterfaces = int32(len(meshInterfaces))

	meshCfg, err := batmanadv.GetMeshConfig(s.Cfg.GetAlfredBatInterface())
	if err != nil {
		s.Log.Error().Err(err).Msg("Error getting mesh config")

		return nil, err
	}

	isMeshGateway = meshCfg.IsGatewayMode()

	position := s.GPS.GetPosition()

	// For now, just return a static status
	return &serviceproto.GetServiceStatusResponse{
		Status: &serviceproto.ServiceStatus{
			IsConnected:          meshConnected,
			ConnectedNeighbors:   connectedNeighbors,
			ActiveMeshInterfaces: numMeshInterfaces,
			IsMeshGateway:        isMeshGateway,
			Position: &serviceproto.Position{
				Latitude:         position.Latitude,
				Longitude:        position.Longitude,
				Altitude:         float32(position.Altitude),
				SatellitesInView: int32(position.SatellitesUsed),
			},
		},
	}, nil
}
