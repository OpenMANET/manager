package mgmt

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/openmanet/go-alfred"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/system"
	"github.com/openmanet/openmanetd/internal/util"
)

type AddressReservationWorker struct {
	Config       *ManagementConfig
	Client       *alfred.Client
	ShutdownChan <-chan os.Signal

	reserveInterval time.Duration
}

func NewAddressReservationWorker(config *ManagementConfig, client *alfred.Client, shutdownChan <-chan os.Signal) *AddressReservationWorker {
	config.Log.Info().Msg("AddressReservationWorker initialized")

	return &AddressReservationWorker{
		Config:       config,
		Client:       client,
		ShutdownChan: shutdownChan,

		reserveInterval: config.addressReservationWorkerReserveInterval,
	}
}

func (arw *AddressReservationWorker) ReserveAddressIfNeeded() {
	var (
		ticker             = time.NewTicker(arw.reserveInterval)
		ipConflictDetected = false
	)

	defer ticker.Stop()

	for {
		select {
		case <-arw.ShutdownChan:
			return
		case <-ticker.C:
			configured, err := network.IsDHCPConfiguredWithReader(arw.Config.uciOpenMANETConfig)
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error checking DHCP configuration")

				continue
			}

			nodes, err := arw.Config.DB.ListMeshNodes(context.Background())
			if err != nil {
				arw.Config.Log.Error().Err(err).Msg("Error listing mesh nodes from database")

				continue
			}

			// Get interface information
			iface := network.GetInterfaceByName(arw.Config.IFace)

			// Check for IP conflicts
			for _, node := range nodes {
				for _, ipAddr := range iface.IP {
					if ipAddr.IP.String() == node.IpAddr {
						ipConflictDetected = true

						arw.Config.Log.Warn().Msgf("IP conflict detected with node %s (%s)", node.Hostname, node.IpAddr)

						break
					}
				}
			}

			if !configured || ipConflictDetected {
				arw.Config.Log.Debug().Msg("DHCP not configured or IP conflict detected, reserving address")

				// Get mesh config to determine if we are a gateway
				meshCfg, err := batmanadv.GetMeshConfig(arw.Config.BatInterface)
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error getting mesh config")

					continue
				}

				staticIP, err := network.SelectAvailableStaticIPFromNodeData(nodes, meshCfg.IsGatewayMode())
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error selecting available static IP")

					continue
				}

				// if arw.Config.IFace is prefixed with "br-", remove the prefix because dhcp and network config is tied to the physical interface
				normalizedIface, err := util.InterfaceWithoutBridge(arw.Config.IFace)
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error normalizing interface name")

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
				}, arw.Config.uciNetworkConfig); err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error setting network config for address reservation")

					continue
				}

				// Process received address reservation records
				dhcpStart, err := network.CalculateAvailableDHCPStart(nodes, network.DefaultNetworkAddress, network.DefaultNetworkMask, network.DefaultDHCPAddressLimit)
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

				arw.Config.Log.Info().Msgf("Static IP %s and DHCP configured via address reservation", staticIP)

				// Mark DHCP as configured
				err = network.SetDHCPConfiguredWithReader(arw.Config.uciOpenMANETConfig)
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error marking DHCP as configured")

					continue
				}

				// Clean up interfaces or configs if needed.
				// This will only happen on initial configuration. If users create things later
				// we will not change them unless they re-request an address reservation.
				err = arw.cleanUpInterfaces()
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error cleaning up interfaces")

					continue
				}

				// Restart the system to apply new network settings
				arw.Config.Log.Info().Msg("Rebooting system to apply new network settings")

				err = system.Reboot()
				if err != nil {
					arw.Config.Log.Error().Err(err).Msg("Error rebooting system")

					continue
				}
			}
		}
	}
}

func (arw *AddressReservationWorker) cleanUpInterfaces() error {
	meshCfg, err := batmanadv.GetMeshConfig(arw.Config.BatInterface)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	if meshCfg.IsGatewayMode() {
		arw.Config.Log.Info().Msg("Mesh gateway mode enabled, skipping interface cleanup")

		return nil
	}

	// Clean up 'lan' network sections if they exist
	if network.NetworkSectionExistsWithReader("lan", arw.Config.uciNetworkConfig) {
		arw.Config.Log.Info().Msg("Removing 'lan' network section")

		if err := network.DeleteNetworkConfigWithReader("lan", arw.Config.uciNetworkConfig); err != nil {
			return fmt.Errorf("error deleting 'lan' network section: %w", err)
		}
	}

	// Commit network changes
	arw.Config.uciNetworkConfig.Commit()

	// Clean up DHCP sections if they exist
	if network.DHCPSectionExistsWithReader("lan", arw.Config.uciDHCPConfig) {
		arw.Config.Log.Info().Msg("Removing 'lan' DHCP section")

		if err := network.DeleteDHCPConfigWithReader("lan", arw.Config.uciDHCPConfig); err != nil {
			return fmt.Errorf("error deleting 'lan' DHCP section: %w", err)
		}
	}

	// Commit DHCP changes
	arw.Config.uciDHCPConfig.Commit()

	// Reload network to apply changes
	err = network.ReloadNetwork()
	if err != nil {
		return fmt.Errorf("error reloading network configuration: %w", err)
	}

	return nil
}
