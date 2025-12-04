package openmanet

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/common-nighthawk/go-figure"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/ptt"
	"github.com/openmanet/openmanetd/internal/util/logger"
)

func Start() {
	var (
		ctx    = context.Background()
		banner = figure.NewFigure("OpenMANET", "big", true)
		log    = logger.InitLogging(ctx)
		c      = make(chan os.Signal, 1)
		cfg    = config.New(nil)
	)

	banner.Print()

	ptt := ptt.NewPTT(ptt.PTTConfig{
		Interupt:      c,
		Log:           logger.GetLogger("ptt"),
		Enable:        cfg.GetPTTEnable(),
		Iface:         cfg.GetMeshNetInterface(),
		McastAddr:     cfg.GetPTTMcastAddr(),
		McastPort:     cfg.GetPTTMcastPort(),
		PttKey:        cfg.GetPTTPttKey(),
		Debug:         cfg.GetPTTDebug(),
		Loopback:      cfg.GetPTTLoopback(),
		PttDevice:     cfg.GetPTTPttDevice(),
		PttDeviceName: cfg.GetPTTPttDeviceName(),
	})

	ptt.Start()

	mgmt := mgmt.NewManager(mgmt.ManagementConfig{
		InteruptChan:               c,
		Log:                        logger.GetLogger("mgmt"),
		GatewayMode:                cfg.GetGatewayMode(),
		AlfredMode:                 cfg.GetAlfredMode(),
		IFace:                      cfg.GetMeshNetInterface(),
		BatInterface:               cfg.GetAlfredBatInterface(),
		SocketPath:                 cfg.GetAlfredSocketPath(),
		GatewayDataType:            cfg.GetAlfredDataTypeGateway(),
		NodeDataType:               cfg.GetAlfredDataTypeNode(),
		PositionDataType:           cfg.GetAlfredDataTypePosition(),
		AddressReservationDataType: cfg.GetAlfredDataTypeAddressReservation(),
	})

	mgmt.Start()

	// Clear the batman-adv hosts file on startup
	// to remove any stale entries
	// Stale entries can cause issues with name resolution for nodes that have changed IPs
	// This can also cause issues with gateway selection if the stale entry is for a gateway node
	err := batmanadv.ClearBatHosts()
	if err != nil {
		log.Error().Err(err).Msg("Error clearing batman-adv hosts file on startup")
	}

	// Wait for interrupt signal to gracefully shutdown the application
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	log.Info().Msg("Exiting OpenMANETd")
}
