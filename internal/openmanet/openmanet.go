package openmanet

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/common-nighthawk/go-figure"
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
	)

	banner.Print()

	ptt := ptt.NewPTT(ptt.PTTConfig{
		Log:       logger.GetLogger("ptt"),
		Enable:    viper.GetBool("ptt.enable"),
		Iface:     viper.GetString("netInterface"),
		McastAddr: viper.GetString("ptt.mcastAddr"),
		McastPort: viper.GetInt("ptt.mcastPort"),
		PttKey:    viper.GetString("ptt.pttKey"),
		Debug:     viper.GetBool("ptt.debug"),
		Loopback:  viper.GetBool("ptt.loopback"),
		PttDevice: viper.GetString("ptt.pttDevice"),
	})

	ptt.Start()

	mgmt := mgmt.NewManager(mgmt.ManagementConfig{
		Log:                        logger.GetLogger("mgmt"),
		Mode:                       viper.GetString("alfred.mode"),
		IFace:                      viper.GetString("netInterface"),
		BatInterface:               viper.GetString("alfred.batInterface"),
		SocketPath:                 viper.GetString("alfred.socketPath"),
		GatewayDataType:            viper.GetBool("alfred.dataTypes.gateway"),
		NodeDataType:               viper.GetBool("alfred.dataTypes.node"),
		PositionDataType:           viper.GetBool("alfred.dataTypes.position"),
		AddressReservationDataType: viper.GetBool("alfred.dataTypes.addressReservation"),
	})

	mgmt.Start()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	log.Info().Msg("Exiting OpenMANETd")
}
