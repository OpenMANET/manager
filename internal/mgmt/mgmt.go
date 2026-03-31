package mgmt

import (
	"context"
	"time"

	"github.com/openmanet/go-alfred"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/util/board"
	"github.com/rs/zerolog"
)

const (
	nodeDataWorkerInterval time.Duration = 60 * time.Second

	gatewayDataWorkerSendInterval time.Duration = 60 * time.Second
	gatewayDataWorkerRecvInterval time.Duration = 10 * time.Second

	addressReservationWorkerReserveInterval time.Duration = 125 * time.Second
)

type ManagementConfig struct {
	Log                                     zerolog.Logger
	DB                                      *models.Queries
	GPS                                     *gpsd.GPSService
	uciOpenMANETConfig                      *network.UCIOpenMANETConfigReader
	uciDHCPConfig                           *network.UCIDHCPConfigReader
	uciNetworkConfig                        *network.UCINetworkConfigReader
	boardConfigInfo                         *board.Board
	WirelessConfig                          *WirelessConfig
	BatInterface                            string
	AlfredMode                              string
	SocketPath                              string
	IFace                                   string
	gatewayWorkerSendInterval               time.Duration
	gatewayWorkerRecvInterval               time.Duration
	addressReservationWorkerReserveInterval time.Duration
	GatewayDataType                         bool
	NodeDataType                            bool
	PositionDataType                        bool
	AddressReservationDataType              bool
	BatmanMulticastEnhancementsEnabled      bool
}

func NewManager(cfg ManagementConfig) (*ManagementConfig, error) {
	boardConfigInfo, err := board.NewBoardConfigInfo()
	if err != nil {
		cfg.Log.Error().Err(err).Msg("Failed to load board configuration")
	}

	// Init nl80211 wirelsss client
	wirelessConfig, err := NewWirelessConfig()
	if err != nil {
		cfg.Log.Error().Err(err).Msg("Failed to load wireless configuration")
	}

	return &ManagementConfig{
		Log:                        cfg.Log,
		AlfredMode:                 cfg.AlfredMode,
		IFace:                      cfg.IFace,
		BatInterface:               cfg.BatInterface,
		SocketPath:                 cfg.SocketPath,
		GatewayDataType:            cfg.GatewayDataType,
		NodeDataType:               cfg.NodeDataType,
		PositionDataType:           cfg.PositionDataType,
		AddressReservationDataType: cfg.AddressReservationDataType,
		WirelessConfig:             wirelessConfig,
		DB:                         cfg.DB,
		GPS:                        cfg.GPS,

		gatewayWorkerSendInterval:               gatewayDataWorkerSendInterval,
		gatewayWorkerRecvInterval:               gatewayDataWorkerRecvInterval,
		addressReservationWorkerReserveInterval: addressReservationWorkerReserveInterval,

		uciOpenMANETConfig: network.NewUCIOpenMANETConfigReader(),
		uciDHCPConfig:      network.NewUCIDHCPConfigReader(),
		uciNetworkConfig:   network.NewUCINetworkConfigReader(),

		boardConfigInfo: boardConfigInfo,
	}, nil
}

func (m *ManagementConfig) Start(ctx context.Context) {
	if err := m.setTransportInterfaceMTU(); err != nil {
		m.Log.Error().Err(err).Msg("Failed to set MTU for transport interface")
	}

	if m.BatmanMulticastEnhancementsEnabled {
		if err := m.configureDeviceMulticast(ctx); err != nil {
			m.Log.Error().Err(err).Msg("Failed to configure device multicast settings")
		}
	}

	if err := m.setupBatMesh1Interface(ctx); err != nil {
		m.Log.Error().Err(err).Msg("Failed to setup batmesh1 interface")
	}

	client, err := alfred.NewClient(alfred.WithSocketPath(m.SocketPath))
	if err != nil {
		m.Log.Fatal().Err(err).Msg("Failed to create Alfred client")
	}

	m.Log.Info().Msg("Alfred Client Started")

	if m.AddressReservationDataType {
		addressReservationWorker := NewAddressReservationWorker(m, client, ctx)
		go addressReservationWorker.ReserveAddressIfNeeded(ctx)
	}

	if m.NodeDataType {
		// Start the node data worker
		nodeDataWorker := NewNodeDataWorker(m, client, nodeDataWorkerInterval, ctx)
		go nodeDataWorker.StartSend()
		go nodeDataWorker.StartReceive()
	}

	if m.GatewayDataType {
		// Start the gateway worker
		gatewayDataWorker := NewGatewayWorker(m, client, ctx)
		go gatewayDataWorker.StartSend()
		go gatewayDataWorker.StartReceive()
	}
}
