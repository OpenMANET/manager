package mgmt

import (
	"os"
	"time"

	"github.com/openmanet/go-alfred"
	"github.com/rs/zerolog"
)

const (
	nodeDataWorkerInterval time.Duration = 60 * time.Second

	gatewayDataWorkerSendInterval time.Duration = 60 * time.Second
	gatewayDataWorkerRecvInterval time.Duration = 10 * time.Second

	addressReservationWorkerSendInterval time.Duration = 10 * time.Second
	addressReservationWorkerRecvInterval time.Duration = 5 * time.Second
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
	InteruptChan               chan os.Signal

	gatewayWorkerSendInterval time.Duration
	gatewayWorkerRecvInterval time.Duration

	addressReservationWorkerSendInterval time.Duration
	addressReservationWorkerRecvInterval time.Duration
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
		InteruptChan:               cfg.InteruptChan,
		GatewayMode:                cfg.GatewayMode,

		gatewayWorkerSendInterval:            gatewayDataWorkerSendInterval,
		gatewayWorkerRecvInterval:            gatewayDataWorkerRecvInterval,
		addressReservationWorkerSendInterval: addressReservationWorkerSendInterval,
		addressReservationWorkerRecvInterval: addressReservationWorkerRecvInterval,
	}
}

func (m *ManagementConfig) Start() {
	client, err := alfred.NewClient(alfred.WithSocketPath(m.SocketPath))
	if err != nil {
		m.Log.Fatal().Err(err).Msg("Failed to create Alfred client")
	}

	m.Log.Info().Msg("Alfred Client Started")

	if m.AddressReservationDataType {
		addressReservationWorker := NewAddressReservationWorker(m, client, m.InteruptChan)
		go addressReservationWorker.StartSend()
		go addressReservationWorker.StartReceive()
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
