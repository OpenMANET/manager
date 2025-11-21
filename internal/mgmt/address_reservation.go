package mgmt

import (
	"os"
	"time"

	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	AddressReservationDataType        uint8 = 104
	AddressReservationDataTypeVersion uint8 = 1
)

type AddressReservationWorker struct {
	Config       ManagementConfig
	Client       *alfred.Client
	ShutdownChan <-chan os.Signal

	sendInterval time.Duration
	recvInterval time.Duration
}

func NewAddressReservationWorker(config *ManagementConfig, client *alfred.Client, shutdownChan <-chan os.Signal) *AddressReservationWorker {
	config.Log.Info().Msg("AddressReservationWorker initialized")

	return &AddressReservationWorker{
		Config:       *config,
		Client:       client,
		ShutdownChan: shutdownChan,

		sendInterval: config.gatewayWorkerSendInterval,
		recvInterval: config.gatewayWorkerRecvInterval,
	}
}

// Start begins the periodic sending of address reservation data to the Alfred client.
func (arw *AddressReservationWorker) StartSend() {
	ticker := time.NewTicker(arw.sendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-arw.ShutdownChan:
			return
		case <-ticker.C:
			// Send address reservation data to the Alfred client
			iface := network.GetInterfaceByName(arw.Config.IFace)
			// Verify that the interface has an IP address
			if len(iface.IP) == 0 {
				arw.Config.Log.Warn().Msgf("Interface %s has no IP address", arw.Config.IFace)
				continue
			}

			ip := iface.IP[0].IP
			if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.To4() == nil {
				arw.Config.Log.Warn().Msgf("Interface %s has no valid IPv4 address", arw.Config.IFace)
				continue
			}

			cidr := iface.GetCIDR()

			addrResData := proto.AddressReservation{
				Mac:             iface.MAC,
				StaticIp:        iface.IP[0].IP.String(),
				ReservationCidr: cidr[0],
			}

			var addrResDataBytes []byte
			var err error
			addrResDataBytes, err = addrResData.MarshalVT()
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error marshaling address reservation data")
				continue
			}

			err = arw.Client.Set(AddressReservationDataType, AddressReservationDataTypeVersion, addrResDataBytes)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error sending address reservation data")
			}
		}
	}
}

// Start begins the periodic receiving of address reservation data from the Alfred client.
func (arw *AddressReservationWorker) StartReceive() {
	ticker := time.NewTicker(arw.recvInterval)
	defer ticker.Stop()

	for {
		select {
		case <-arw.ShutdownChan:
			return
		case <-ticker.C:
			// Get address reservation data from the Alfred client
			record, err := arw.Client.Request(AddressReservationDataType)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error receiving address reservation data")
			} else {
				// Process received address reservation records
				for _, rec := range record {
					var addrResData proto.AddressReservation
					err = addrResData.UnmarshalVT(rec.Data)
					if err != nil {
						arw.Config.Log.Error().Err(err).Msg("Error unmarshaling address reservation data")
						continue
					}

					// TODO: Handle address reservation data
					arw.Config.Log.Debug().Msgf("Received address reservation: %+v", &addrResData)
				}
			}
		}
	}
}
