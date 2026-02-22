package mgmt

import (
	"fmt"
	"os"
	"time"

	"github.com/openmanet/go-alfred"
	"github.com/openmanet/openmanetd/internal/database/models"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/security"
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

	gatewayWorkerSendInterval time.Duration
	gatewayWorkerRecvInterval time.Duration

	addressReservationWorkerReserveInterval time.Duration

	GatewayMode                bool
	GatewayDataType            bool
	NodeDataType               bool
	PositionDataType           bool
	AddressReservationDataType bool

	payloadCodec *security.PayloadCodec
}

func NewManager(cfg ManagementConfig) (*ManagementConfig, error) {
	boardConfigInfo, err := board.NewBoardConfigInfo()
	if err != nil {
		cfg.Log.Error().Err(err).Msg("Failed to load board configuration")
	}

	meshPassphrase, err := network.GetWirelessMeshPassphrase()
	if err != nil {
		return nil, fmt.Errorf("failed to read mesh passphrase from wireless config: %w", err)
	}

	payloadCodec, err := security.NewPayloadCodecFromPassphrase(meshPassphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize payload security: %w", err)
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
		payloadCodec:    payloadCodec,
	}, nil
}

func (m *ManagementConfig) Start() {
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
