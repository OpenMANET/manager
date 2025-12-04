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
	AddressReservationDataType        uint8 = uint8(proto.DataType_DATA_TYPE_ADDRESS_RESERVATION)
	AddressReservationDataTypeVersion uint8 = 1
)

type AddressReservationWorker struct {
	Config       *ManagementConfig
	Client       *alfred.Client
	ShutdownChan <-chan os.Signal

	sendInterval time.Duration
	recvInterval time.Duration
}

func NewAddressReservationWorker(config *ManagementConfig, client *alfred.Client, shutdownChan <-chan os.Signal) *AddressReservationWorker {
	config.Log.Info().Msg("AddressReservationWorker initialized")

	return &AddressReservationWorker{
		Config:       config,
		Client:       client,
		ShutdownChan: shutdownChan,

		sendInterval: config.addressReservationWorkerSendInterval,
		recvInterval: config.addressReservationWorkerRecvInterval,
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

			configured, err := network.IsDHCPConfiguredWithReader(arw.Config.uciOpenMANETConfig)
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
				normalizedIface string
				iface           = network.GetInterfaceByName(arw.Config.IFace)
			)

			// Get address reservation data from the Alfred client
			records, err := arw.Client.Request(AddressReservationDataType)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error receiving address reservation data")
				continue
			}

			configured, err := network.IsDHCPConfiguredWithReader(arw.Config.uciOpenMANETConfig)
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
					// only respond to requests not from ourselves
					if addrRes.RequestingReservation && addrRes.Mac != iface.MAC {

						arw.Config.Log.Debug().Interface("addressRes", &addrRes).Msg("Processing address reservation request")

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

			// DHCP and the Static IP are not configured, process received records to configure them
			// If we are a mesh gateway, skip receiving
			meshCfg, err := batmanadv.GetMeshConfig(arw.Config.BatInterface)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error getting mesh config")
				continue
			}

			// if arw.Config.IFace is prefixed with "br-", remove the prefix because dhcp and network config is tied to the physical interface
			if after, ok := strings.CutPrefix(arw.Config.IFace, "br-"); ok {
				normalizedIface = after
			}

			staticIP, err := network.SelectAvailableStaticIP(records, meshCfg.IsGatewayMode())
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error selecting available static IP")
				continue
			}

			if err := network.SetNetworkConfigWithReader(normalizedIface, &network.UCINetwork{
				Proto:          network.DefaultNetworkProto,
				IPAddr:         staticIP,
				NetMask:        network.DefaultNetworkMask,
				IPV6Class:      network.DefaultIPv6Class,
				IPV6IfaceID:    network.DefaultIPv6IfaceID,
				IPV6Assignment: network.DefaultIPv6Assign,
				Device:         arw.Config.IFace,
				DNS:            "1.1.1.1",
			}, arw.Config.uciNetworkConfig); err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error setting network config for address reservation")
				continue
			}

			// Process received address reservation records
			dhcpStart, err := network.CalculateAvailableDHCPStart(records, network.DefaultNetworkAddress, network.DefaultNetworkMask, network.DefaultDHCPAddressLimit)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error calculating available DHCP start address")
				continue
			}

			dhcpConfig := &network.UCIDHCP{
				Interface: normalizedIface,
				Start:     strconv.Itoa(dhcpStart),
				Limit:     strconv.Itoa(network.DefaultDHCPAddressLimit),
				LeaseTime: network.DefaultDHCPLeaseTime,
				Force:     "1",
			}

			arw.Config.Log.Debug().Interface("dhcpConfig", dhcpConfig).Msg("Setting DHCP config")

			err = network.SetDHCPConfigWithReader(normalizedIface, dhcpConfig, arw.Config.uciDHCPConfig)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error setting DHCP config")
				continue
			}

			// Reload network to apply changes
			err = network.ReloadNetwork()
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error reloading network configuration")
				continue
			}

			arw.Config.Log.Info().Msgf("Static IP %s and DHCP configured via address reservation", staticIP)

			// Mark DHCP as configured
			err = network.SetDHCPConfiguredWithReader(arw.Config.uciOpenMANETConfig)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error marking DHCP as configured")
				continue
			}
		}
	}
}

// createAddressReservationResponse generates a serialized AddressReservation protobuf message
// containing the network interface configuration details. It retrieves the MAC address, IP address,
// CIDR notation, and DHCP configuration (start address and limit) for the configured interface.
//
// If the interface name is prefixed with "br-", the prefix is removed before querying DHCP configuration,
// as DHCP config is associated with the physical interface rather than the bridge.
//
// Returns the marshaled protobuf bytes and an error if:
//   - DHCP configuration cannot be retrieved
//   - The interface has no IP address
//   - The interface has no valid IPv4 address (unspecified, loopback, or non-IPv4)
//   - Marshaling the protobuf message fails
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
