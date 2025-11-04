package openmanet

import (
	"github.com/common-nighthawk/go-figure"
	alfred "github.com/openmanet/go-alfred"
)

func Start() {
	banner := figure.NewFigure("OpenMANET", "big", true)
	banner.Print()

	client, err := alfred.NewClient()
	if err != nil {
		panic(err)
	}

	defer client.Close()

}
