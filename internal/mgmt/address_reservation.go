package mgmt

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	AddressReservationDataType        uint8 = 104
	AddressReservationDataTypeVersion uint8 = 1

	defaultNetworkAddress string = "10.41.0.0"
	defaultNetworkMask    string = "255.255.0.0"
	defaultAddressLimit   int    = 16

	defaultDHCPLeaseTime string = "12h"
)

type AddressReservationWorker struct {
	Config       ManagementConfig
	Client       *alfred.Client
	ShutdownChan <-chan os.Signal

	uciOpenMANETConfig *network.UCIOpenMANETConfigReader
	uciDHCPConfig      *network.UCIDHCPConfigReader
	sendInterval       time.Duration
	recvInterval       time.Duration
}

func NewAddressReservationWorker(config *ManagementConfig, client *alfred.Client, shutdownChan <-chan os.Signal) *AddressReservationWorker {
	config.Log.Info().Msg("AddressReservationWorker initialized")

	return &AddressReservationWorker{
		Config:       *config,
		Client:       client,
		ShutdownChan: shutdownChan,

		uciOpenMANETConfig: network.NewUCIOpenMANETConfigReader(),
		uciDHCPConfig:      network.NewUCIDHCPConfigReader(),
		sendInterval:       config.addressReservationWorkerSendInterval,
		recvInterval:       config.addressReservationWorkerRecvInterval,
	}
}

// Start begins the periodic sending of address reservation requests to the Alfred client.
func (arw *AddressReservationWorker) StartSend() {
	ticker := time.NewTicker(arw.sendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-arw.ShutdownChan:
			return
		case <-ticker.C:
			var (
				err error
			)
			// If we are a mesh gateway, skip sending
			meshCfg, err := batmanadv.GetMeshConfig(arw.Config.BatInterface)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error getting mesh config")
				continue
			}

			// If we are NOT in gateway mode, ensure DHCP is configured
			if !meshCfg.IsGatewayMode() {
				configured, err := network.IsDHCPConfiguredWithReader(arw.uciOpenMANETConfig)
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error checking DHCP configuration")
					continue
				}

				// If DHCP is not configured, send address reservation request
				if !configured {
					arw.Config.Log.Debug().Msg("DHCP is not configured, sending address reservation request")

					iface := network.GetInterfaceByName(arw.Config.IFace)

					addrResData := proto.AddressReservation{
						Mac:                   iface.MAC,
						StaticIp:              iface.IP[0].IP.String(),
						RequestingReservation: true,
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

					arw.Config.Log.Debug().Interface("addressRes", &addrResData).Msg("Address reservation request sent")
				}
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
				iface     = network.GetInterfaceByName(arw.Config.IFace)
			)

			// Get address reservation data from the Alfred client
			records, err := arw.Client.Request(AddressReservationDataType)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error receiving address reservation data")
				continue
			}

			configured, err := network.IsDHCPConfiguredWithReader(arw.uciOpenMANETConfig)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error checking DHCP configuration")
				continue
			}

			// If DHCP is configured already, process records to see if there are any requests for reservations
			if configured {
				for _, record := range records {
					var addrRes proto.AddressReservation
					if err := addrRes.UnmarshalVT(record.Data); err != nil {
						arw.Config.Log.Error().Err(err).Msg("Error unmarshaling address reservation data")
						continue
					}

					// If there is a reservation request, process it
					if addrRes.RequestingReservation {
						arw.Config.Log.Debug().Interface("addressRes", &addrRes).Msg("Processing address reservation request")

						// Skip if the request is from ourselves
						if addrRes.Mac == iface.MAC {
							continue
						}

						// Create and send address reservation response
						addrResDataBytes, err := arw.createAddressReservationResponse()
						if err != nil {
							arw.Config.Log.Error().Err(err).Msg("Error creating address reservation response")
							continue
						}

						err = arw.Client.Set(AddressReservationDataType, AddressReservationDataTypeVersion, addrResDataBytes)
						if err != nil {
							arw.Config.Log.Error().Err(err).Msg("Error sending address reservation response")
							continue
						}

						arw.Config.Log.Debug().Msg("Address reservation response sent")
					}
				}

				// DHCP is already configured, skip further processing
				continue
			}

			// If we are a mesh gateway, skip receiving
			meshCfg, err := batmanadv.GetMeshConfig(arw.Config.BatInterface)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error getting mesh config")
				continue
			}

			if meshCfg.IsGatewayMode() {
				arw.Config.Log.Debug().Msg("Node is in gateway mode, skipping address reservation")
				// ticker.Stop()

				continue
			}

			// Process received address reservation records
			dhcpStart, err := network.CalculateAvailableDHCPStart(records, defaultNetworkAddress, defaultNetworkMask, defaultAddressLimit)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error calculating available DHCP start address")
				continue
			}

			// if arw.Config.IFace is prefixed with "br-", remove the prefix because dhcp config is tied to the physical interface
			if after, ok := strings.CutPrefix(arw.Config.IFace, "br-"); ok {
				dhcpiface = after
			}

			dhcpConfig := &network.UCIDHCP{
				Interface: dhcpiface,
				Start:     strconv.Itoa(dhcpStart),
				Limit:     strconv.Itoa(defaultAddressLimit),
				LeaseTime: defaultDHCPLeaseTime,
				Force:     "1",
			}

			arw.Config.Log.Debug().Interface("dhcpConfig", dhcpConfig).Msg("Setting DHCP config")

			err = network.SetDHCPConfigWithReader(dhcpiface, dhcpConfig, arw.uciDHCPConfig)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error setting DHCP config")
				continue
			}

			err = network.SetDHCPConfiguredWithReader(arw.uciOpenMANETConfig)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error marking DHCP as configured")
				continue
			}
		}
	}
}

func (arw *AddressReservationWorker) createAddressReservationResponse() ([]byte, error) {
	var (
		dhcpiface string
	)
	iface := network.GetInterfaceByName(arw.Config.IFace)

	// if arw.Config.IFace is prefixed with "br-", remove the prefix because dhcp config is tied to the physical interface
	if after, ok := strings.CutPrefix(arw.Config.IFace, "br-"); ok {
		dhcpiface = after
	}

	dhcp, err := network.GetDHCPConfig(dhcpiface)
	if err != nil {
		return nil, err
	}

	// Verify that the interface has an IP address
	if len(iface.IP) == 0 {
		return nil, fmt.Errorf("interface %s has no IP address", arw.Config.IFace)
	}

	ip := iface.IP[0].IP

	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.To4() == nil {
		arw.Config.Log.Warn().Msgf("Interface %s has no valid IPv4 address", arw.Config.IFace)
		return nil, fmt.Errorf("interface %s has no valid IPv4 address", arw.Config.IFace)
	}

	cidr := iface.GetCIDR()

	addrResData := proto.AddressReservation{
		Mac:                   iface.MAC,
		StaticIp:              iface.IP[0].IP.String(),
		ReservationCidr:       cidr[0],
		UciDhcpStart:          dhcp.Start,
		UciDhcpLimit:          dhcp.Limit,
		RequestingReservation: false,
	}

	var addrResDataBytes []byte
	addrResDataBytes, err = addrResData.MarshalVT()
	if err != nil {
		return nil, fmt.Errorf("error marshaling address reservation data: %w", err)
	}

	return addrResDataBytes, nil
}
