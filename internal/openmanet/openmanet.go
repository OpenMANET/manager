package openmanet

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/common-nighthawk/go-figure"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/ptt"
	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/spf13/viper"
)

func Start() {
	var (
		ctx    = context.Background()
		banner = figure.NewFigure("OpenMANET", "big", true)
		log    = logger.InitLogging(ctx)
		c      = make(chan os.Signal, 1)
	)

	banner.Print()

	ptt := ptt.NewPTT(ptt.PTTConfig{
		Interupt:      c,
		Log:           logger.GetLogger("ptt"),
		Enable:        viper.GetBool("ptt.enable"),
		Iface:         viper.GetString("meshNetInterface"),
		McastAddr:     viper.GetString("ptt.mcastAddr"),
		McastPort:     viper.GetInt("ptt.mcastPort"),
		PttKey:        viper.GetString("ptt.pttKey"),
		Debug:         viper.GetBool("ptt.debug"),
		Loopback:      viper.GetBool("ptt.loopback"),
		PttDevice:     viper.GetString("ptt.pttDevice"),
		PttDeviceName: viper.GetString("ptt.pttDeviceName"),
	})

	ptt.Start()

	mgmt := mgmt.NewManager(mgmt.ManagementConfig{
		InteruptChan:               c,
		Log:                        logger.GetLogger("mgmt"),
		GatewayMode:                viper.GetBool("gatewayMode"),
		AlfredMode:                 viper.GetString("alfred.mode"),
		IFace:                      viper.GetString("meshNetInterface"),
		BatInterface:               viper.GetString("alfred.batInterface"),
		SocketPath:                 viper.GetString("alfred.socketPath"),
		GatewayDataType:            viper.GetBool("alfred.dataTypes.gateway"),
		NodeDataType:               viper.GetBool("alfred.dataTypes.node"),
		PositionDataType:           viper.GetBool("alfred.dataTypes.position"),
		AddressReservationDataType: viper.GetBool("alfred.dataTypes.addressReservation"),
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
