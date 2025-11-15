package mgmt

import (
	"time"

	"github.com/openmanet/go-alfred"
	"github.com/rs/zerolog"
)

type ManagementConfig struct {
	Log                        zerolog.Logger
	Mode                       string
	IFace                      string
	BatInterface               string
	SocketPath                 string
	GatewayDataType            bool
	NodeDataType               bool
	PositionDataType           bool
	AddressReservationDataType bool
}

func NewManager(cfg ManagementConfig) *ManagementConfig {
	return &ManagementConfig{
		Log:                        cfg.Log,
		Mode:                       cfg.Mode,
		IFace:                      cfg.IFace,
		BatInterface:               cfg.BatInterface,
		SocketPath:                 cfg.SocketPath,
		GatewayDataType:            cfg.GatewayDataType,
		NodeDataType:               cfg.NodeDataType,
		PositionDataType:           cfg.PositionDataType,
		AddressReservationDataType: cfg.AddressReservationDataType,
	}
}

func (m *ManagementConfig) Start() {
	client, err := alfred.NewClient(alfred.WithSocketPath(m.SocketPath))
	if err != nil {
		panic(err)
	}
	//defer client.Close()

	/* 	switch cfg.Mode {
	   	case "primary":
	   		err = client.ModeSwitch(alfred.ModePrimary)
	   	case "secondary":
	   		err = client.ModeSwitch(alfred.ModeSecondary)
	   	default:
	   		panic("invalid alfred mode in config")
	   	}
	   	if err != nil {
	   		panic(err)
	   	}

	   	if err = client.ChangeInterfaces(cfg.IFace); err != nil {
	   		panic(err)
	   	}

	   	if err = client.ChangeBatmanInterface(cfg.BatInterface); err != nil {
	   		panic(err)
	   	}
	*/
	m.Log.Info().Msg("Alfred Client Started")

	nodeDataWorker := NewNodeDataWorker(m, client, 5*time.Second, make(chan any))

	go nodeDataWorker.StartSend()
	go nodeDataWorker.StartReceive()

}
