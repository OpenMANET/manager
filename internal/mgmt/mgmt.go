package mgmt

import (
	"os"
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
	Log          zerolog.Logger
	DB           *models.Queries
	InteruptChan chan os.Signal

	GPS *gpsd.GPSService

	uciOpenMANETConfig *network.UCIOpenMANETConfigReader
	uciDHCPConfig      *network.UCIDHCPConfigReader
	uciNetworkConfig   *network.UCINetworkConfigReader

	boardConfigInfo *board.Board
	IFace           string
	AlfredMode      string
	BatInterface    string
	SocketPath      string
	WirelessConfig  *WirelessConfig

	gatewayWorkerSendInterval time.Duration
	gatewayWorkerRecvInterval time.Duration

	addressReservationWorkerReserveInterval time.Duration

	GatewayMode                bool
	GatewayDataType            bool
	NodeDataType               bool
	PositionDataType           bool
	AddressReservationDataType bool
}

func NewManager(cfg ManagementConfig) *ManagementConfig {

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
		InteruptChan:               cfg.InteruptChan,
		GatewayMode:                cfg.GatewayMode,
		DB:                         cfg.DB,
		GPS:                        cfg.GPS,

		gatewayWorkerSendInterval:               gatewayDataWorkerSendInterval,
		gatewayWorkerRecvInterval:               gatewayDataWorkerRecvInterval,
		addressReservationWorkerReserveInterval: addressReservationWorkerReserveInterval,

		uciOpenMANETConfig: network.NewUCIOpenMANETConfigReader(),
		uciDHCPConfig:      network.NewUCIDHCPConfigReader(),
		uciNetworkConfig:   network.NewUCINetworkConfigReader(),

		boardConfigInfo: boardConfigInfo,
	}
}

func (m *ManagementConfig) Start() {
	if err := m.setTransportInterfaceMTU(); err != nil {
		m.Log.Error().Err(err).Msg("Failed to set MTU for transport interface")
	}

	client, err := alfred.NewClient(alfred.WithSocketPath(m.SocketPath))
	if err != nil {
		m.Log.Fatal().Err(err).Msg("Failed to create Alfred client")
	}

	m.Log.Info().Msg("Alfred Client Started")

	if m.AddressReservationDataType {
		addressReservationWorker := NewAddressReservationWorker(m, client, m.InteruptChan)
		go addressReservationWorker.ReserveAddressIfNeeded()
	}

	if m.NodeDataType {
		// Start the node data worker
		nodeDataWorker := NewNodeDataWorker(m, client, nodeDataWorkerInterval, m.InteruptChan)
		go nodeDataWorker.StartSend()
		go nodeDataWorker.StartReceive()

	}

	if m.GatewayDataType {
		// Start the gateway worker
		gatewayDataWorker := NewGatewayWorker(m, client, m.InteruptChan)
		go gatewayDataWorker.StartSend()
		go gatewayDataWorker.StartReceive()
	}
}
