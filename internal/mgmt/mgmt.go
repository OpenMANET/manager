package mgmt

import (
	"time"

	"github.com/openmanet/go-alfred"
	"github.com/rs/zerolog"
)

const (
	nodeDataWorkerInterval    time.Duration = 5 * time.Second
	gatewayDataWorkerSendInterval time.Duration = 60 * time.Second
	gatewayDataWorkerRecvInterval time.Duration = 1 * time.Second
)

type ManagementConfig struct {
	Log                        zerolog.Logger
	GatewayMode                bool
	IFace                      string
	AlfredMode                 string
	BatInterface               string
	SocketPath                 string
	GatewayDataType            bool
	NodeDataType               bool
	PositionDataType           bool
	AddressReservationDataType bool

	gatewayWorkerSendInterval time.Duration
	gatewayWorkerRecvInterval time.Duration
	nodeDataWorkerSendInterval time.Duration
	nodeDataWorkerRecvInterval time.Duration
}

func NewManager(cfg ManagementConfig) *ManagementConfig {
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
		gatewayWorkerSendInterval:  gatewayDataWorkerSendInterval,
		gatewayWorkerRecvInterval:  gatewayDataWorkerRecvInterval,
	}
}

func (m *ManagementConfig) Start() {
	client, err := alfred.NewClient(alfred.WithSocketPath(m.SocketPath))
	if err != nil {
		m.Log.Fatal().Err(err).Msg("Failed to create Alfred client")
	}

	m.Log.Info().Msg("Alfred Client Started")

	nodeDataWorker := NewNodeDataWorker(m, client, nodeDataWorkerInterval, make(chan any))
	go nodeDataWorker.StartSend()
	go nodeDataWorker.StartReceive()

	// Start the gateway worker
	gatewayDataWorker := NewGatewayWorker(m, client, make(chan any))
	go gatewayDataWorker.StartSend()
	go gatewayDataWorker.StartReceive()

}
