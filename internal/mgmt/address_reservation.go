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

	defaultNetworkAddress string = "10.41.0.0"
	defaultNetworkMask    string = "255.255.0.0"
	defaultAddressLimit   int    = 16
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
			var (
				dhcpiface string
				err       error
			)

			configured, err := network.IsDHCPConfigured()
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error checking DHCP configuration")
				continue
			}

			// Skip if DHCP is not configured
			if !configured {
				continue
			}

			iface := network.GetInterfaceByName(arw.Config.IFace)

			// if arw.Config.IFace is prefixed with "br-", remove the prefix because dhcp config is tied to the physical interface
			if len(arw.Config.IFace) > 3 && arw.Config.IFace[:3] == "br-" {
				dhcpiface = arw.Config.IFace[3:]
			}

			dhcp, err := network.GetDHCPConfig(dhcpiface)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msgf("Error getting DHCP config for interface %s", dhcpiface)
				continue
			}

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
				UciDhcpStart:    dhcp.Start,
				UciDhcpLimit:    dhcp.Limit,
			}

			var addrResDataBytes []byte
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
			var (
				dhcpiface string
			)
			// If address is already set, skip receiving
			configured, err := network.IsDHCPConfigured()
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error checking DHCP configuration")
				continue
			}

			// Skip if DHCP is configured
			if configured {
				continue
			}

			// Get address reservation data from the Alfred client
			record, err := arw.Client.Request(AddressReservationDataType)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error receiving address reservation data")
			} else {
				// Process received address reservation records
				dhcpStart, err := network.CalculateAvailableDHCPStart(record, defaultNetworkAddress, defaultNetworkMask, defaultAddressLimit)
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error calculating available DHCP start address")
					continue
				}

				dhcpConfig := &network.UCIDHCP{
					Interface: arw.Config.IFace,
					Start:     string(dhcpStart),
					Limit:     string(defaultAddressLimit),
					LeaseTime: "12h",
					Force:     "1",
				}

				// if arw.Config.IFace is prefixed with "br-", remove the prefix because dhcp config is tied to the physical interface
				if len(arw.Config.IFace) > 3 && arw.Config.IFace[:3] == "br-" {
					dhcpiface = arw.Config.IFace[3:]
				}

				err = network.SetDHCPConfig(dhcpiface, dhcpConfig)
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error setting DHCP config")
					continue
				}

				err = network.SetDHCPConfigured()
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error marking DHCP as configured")
					continue
				}
			}
		}
	}
}
