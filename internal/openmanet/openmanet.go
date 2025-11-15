package openmanet

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/common-nighthawk/go-figure"
	alfred "github.com/openmanet/go-alfred"
	"github.com/openmanet/openmanetd/internal/ptt"
	"github.com/spf13/viper"
)

func Start() {
	banner := figure.NewFigure("OpenMANET", "big", true)
	banner.Print()

	ptt.Start(ptt.Config{
		Enable:    viper.GetBool("ptt.enable"),
		Iface:     viper.GetString("netInterface"),
		McastAddr: viper.GetString("ptt.mcastAddr"),
		McastPort: viper.GetInt("ptt.mcastPort"),
		PttKey:    viper.GetString("ptt.pttKey"),
		Debug:     viper.GetBool("ptt.debug"),
		Loopback:  viper.GetBool("ptt.loopback"),
		PttDevice: viper.GetString("ptt.pttDevice"),
	})

	client, err := alfred.NewClient()
	if err != nil {
		panic(err)
	}

	defer client.Close()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	fmt.Println("Exiting OpenMANETd.")
}
