package handlers

import (
	"context"

	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

type InterfaceService struct {
	Log  zerolog.Logger
	Wifi *mgmt.WirelessConfig
}

func (w *InterfaceService) GetWirelessInterface(_ context.Context, req *serviceproto.GetWirelessInterfaceRequest) (*serviceproto.GetWirelessInterfaceResponse, error) {
	w.Log.Debug().Msg("GetWirelessInterfaces Request Received")

	wifiInterfaces, err := w.Wifi.Interfaces()
	if err != nil {
		w.Log.Error().Err(err).Msg("Failed to get interface")
		return nil, err
	}

	var wifiInt *serviceproto.WirelessInterface
	for _, iface := range wifiInterfaces {
		// find the interface with the requested name
		if req.Name != "" && iface.Name != req.Name {
			continue
		}
		wifiInt = &serviceproto.WirelessInterface{
			Index:           int32(iface.Index),
			Name:            iface.Name,
			HardwareAddress: iface.HardwareAddr.String(),
			Phy:             int32(iface.PHY),
			Device:          int32(iface.Device),
			InterfaceType:   iface.Type.String(),
			Frequency:       int32(iface.Frequency),
			ChannelWidth:    int32(iface.ChannelWidth),
		}
	}

	return &serviceproto.GetWirelessInterfaceResponse{
		Interface: wifiInt,
	}, nil
}

func (w *InterfaceService) ListWirelessInterfaces(_ context.Context, _ *emptypb.Empty) (*serviceproto.ListWirelessInterfacesResponse, error) {
	w.Log.Debug().Msg("ListWirelessInterfaces Request Received")

	wifiInterfaces, err := w.Wifi.GetMeshInterfaces()
	if err != nil {
		w.Log.Error().Err(err).Msg("Failed to list interfaces")
		return nil, err
	}

	var protoInterfaces []*serviceproto.WirelessInterface
	for _, iface := range wifiInterfaces {
		protoInterfaces = append(protoInterfaces, &serviceproto.WirelessInterface{
			Index:           int32(iface.Index),
			Name:            iface.Name,
			HardwareAddress: iface.HardwareAddr.String(),
			Phy:             int32(iface.PHY),
			Device:          int32(iface.Device),
			InterfaceType:   iface.Type.String(),
			Frequency:       int32(iface.Frequency),
			ChannelWidth:    int32(iface.ChannelWidth),
		})
	}

	return &serviceproto.ListWirelessInterfacesResponse{
		Interfaces: protoInterfaces,
	}, nil
}
