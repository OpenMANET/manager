package mgmt

import (
	"net"
	"os"
	"time"

	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/util"
)

const (
	GatewayDataType              uint8 = uint8(proto.DataType_DATA_TYPE_GATEWAY)
	GatewayDataTypeVersion       uint8 = 2
	legacyGatewayDataTypeVersion uint8 = 1
)

type GatewayWorker struct {
	Config       *ManagementConfig
	Client       *alfred.Client
	ShutdownChan <-chan os.Signal

	sendInterval time.Duration
	recvInterval time.Duration
}

func NewGatewayWorker(config *ManagementConfig, client *alfred.Client, shutdownChan <-chan os.Signal) *GatewayWorker {
	config.Log.Info().Msg("GatewayWorker initialized")

	return &GatewayWorker{
		Config:       config,
		Client:       client,
		ShutdownChan: shutdownChan,

		sendInterval: config.gatewayWorkerSendInterval,
		recvInterval: config.gatewayWorkerRecvInterval,
	}
}

// Start begins the periodic sending of gateway data to the Alfred client.
func (gw *GatewayWorker) StartSend() {
	ticker := time.NewTicker(gw.sendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gw.ShutdownChan:
			return
		case <-ticker.C:
			configured, err := network.IsDHCPConfiguredWithReader(gw.Config.uciOpenMANETConfig)
			if err != nil {
				gw.Config.Log.Error().Err(err).Msg("Error checking DHCP configuration")
				continue
			}

			if !configured {
				gw.Config.Log.Debug().Msg("Static Address & DHCP not configured, skipping gateway data send")
				continue
			}

			// Get mesh config from batman-adv to check if we are in gateway mode
			meshCfg, err := batmanadv.GetMeshConfig(gw.Config.BatInterface)
			if err != nil {
				gw.Config.Log.Error().Err(err).Msg("Error getting mesh config")
				continue
			}

			// Only send gateway data if we are in gateway mode
			if meshCfg.IsGatewayMode() {
				iface := network.GetInterfaceByName(gw.Config.IFace)
				hostname, err := os.Hostname()
				if err != nil {
					gw.Config.Log.Error().Err(err).Msg("Error getting hostname")
					hostname = "unknown"
				}

				// Verify that the interface has an IP address
				if len(iface.IP) == 0 {
					gw.Config.Log.Warn().Msgf("Interface %s has no IP address", gw.Config.IFace)
					continue
				}

				// Verify that the interface has a valid IPV4 address
				if iface.IP[0].IP.To4() == nil {
					gw.Config.Log.Warn().Msgf("Interface %s has no valid IPv4 address", gw.Config.IFace)
					continue
				}

				// Prepare gateway data
				gatewayData := proto.Gateway{
					// We use the mesh interface MAC as the gateway identifier
					// Not the br-awhlan MAC.  Batman-adv uses the mesh MAC to identify gateways.
					Mac: meshCfg.HardAddress,
					// Use the IP address of the br-awhlan interface
					// This is to setup routing to the gateway correctly for layer 3
					Ipaddr: iface.IP[0].IP.String(),
					// Use the hostname of the gateway
					Hostname: hostname,
				}

				var gatewayDataBytes []byte
				gatewayDataBytes, err = gatewayData.MarshalVT()
				if err != nil {
					gw.Config.Log.Error().Err(err).Msg("Error marshaling gateway data")
					continue
				}

				encryptedPayload, err := gw.Config.payloadCodec.Encrypt(GatewayDataType, gatewayDataBytes)
				if err != nil {
					gw.Config.Log.Error().Err(err).Msg("Error encrypting gateway data")
					continue
				}

				err = gw.Client.Set(GatewayDataType, GatewayDataTypeVersion, encryptedPayload)
				if err != nil {
					gw.Config.Log.Error().Err(err).Msg("Error sending gateway data")
				}
			}
		}
	}
}

// Start begins the periodic receiving of gateway data from the Alfred client.
func (gw *GatewayWorker) StartReceive() {
	ticker := time.NewTicker(gw.recvInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gw.ShutdownChan:
			return
		case <-ticker.C:
			// If we are not in gateway mode, process received gateway data
			meshCfg, err := batmanadv.GetMeshConfig(gw.Config.BatInterface)
			if err != nil {
				gw.Config.Log.Error().Err(err).Msg("Error getting mesh config")
				continue
			}

			if meshCfg.IsGatewayMode() {
				// Skip processing if we are in gateway mode
				continue
			}

			record, err := gw.Client.Request(GatewayDataType)
			if err != nil {
				gw.Config.Log.Error().Err(err).Msg("Error receiving gateway data")
				continue
			}

			// Get the gateway status from batman-adv
			batGwys, err := batmanadv.GetMeshGateways(gw.Config.BatInterface)
			if err != nil {
				gw.Config.Log.Error().Err(err).Msg("Error getting mesh gateways")
				continue
			}

			// If no gateways are present in batman-adv, skip processing
			if len(*batGwys) == 0 {
				gw.Config.Log.Debug().Msg("No gateways present in batman-adv")
				continue
			}

			// If only one gateway is present from batman-adv, loop through the
			// gateway records and match batman-adv original address MAC to the received gateway MAC
			// This is to identify the active gateway in the mesh
			if len(*batGwys) == 1 {
				batGw := batGwys.GetBest()
				for _, rec := range record {
					gatewayData, ok := gw.decodeGatewayRecord(rec)
					if !ok {
						continue
					}

					if gatewayData.Mac == batGw.OrigAddress {
						// Replace default route with the matched gateway IP
						ipString := net.ParseIP(gatewayData.Ipaddr)

						if ipString != nil {
							if err := network.ReplaceDefaultRoute(ipString, gw.Config.IFace); err != nil {
								gw.Config.Log.Error().Err(err).Msgf("Failed to replace default route with gateway %s", gatewayData.Ipaddr)
							}

							iFaceName, err := util.InterfaceWithoutBridge(gw.Config.IFace)
							if err != nil {
								gw.Config.Log.Error().Err(err).Msg("Error normalizing interface name for DNS setting")
								continue
							}

							if err := network.SetNetworkDNSWithReader(iFaceName, ipString.String(), gw.Config.uciNetworkConfig); err != nil {
								gw.Config.Log.Error().Err(err).Msgf("Failed to set DNS server to gateway %s", gatewayData.Ipaddr)
							}
						}
					}
				}

				// Skip further processing as we have already matched the single gateway
				continue
			}

			if len(*batGwys) > 1 {
				// TODO: Handle multiple gateways in batman-adv
				batGw := batGwys.GetBest()

				gw.Config.Log.Debug().Msg("Multiple gateways present in batman-adv")
				// Process received gateway records
				for _, rec := range record {
					gatewayData, ok := gw.decodeGatewayRecord(rec)
					if !ok {
						continue
					}

					// TODO: Handle multiple gateways in batman-adv
					if gatewayData.Mac == batGw.OrigAddress {
						// Replace default route with the matched gateway IP
						ipString := net.ParseIP(gatewayData.Ipaddr)

						if ipString != nil {
							if err := network.ReplaceDefaultRoute(ipString, gw.Config.IFace); err != nil {
								gw.Config.Log.Error().Err(err).Msgf("Failed to replace default route with gateway %s", gatewayData.Ipaddr)
							}

							iFaceName, err := util.InterfaceWithoutBridge(gw.Config.IFace)
							if err != nil {
								gw.Config.Log.Error().Err(err).Msg("Error normalizing interface name for DNS setting")
								continue
							}

							if err := network.SetNetworkDNSWithReader(iFaceName, ipString.String(), gw.Config.uciNetworkConfig); err != nil {
								gw.Config.Log.Error().Err(err).Msgf("Failed to set DNS server to gateway %s", gatewayData.Ipaddr)
							}
						}

						break
					}
				}
			}
		}
	}
}

func (gw *GatewayWorker) decodeGatewayRecord(rec alfred.Record) (proto.Gateway, bool) {
	decodedPayload := rec.Data

	switch rec.Version {
	case GatewayDataTypeVersion:
		var err error
		decodedPayload, err = gw.Config.payloadCodec.Decrypt(GatewayDataType, rec.Source, rec.Data)
		if err != nil {
			gw.Config.Log.Warn().
				Err(err).
				Str("source", rec.Source.String()).
				Msg("Dropping gateway data payload that failed authentication/decryption")
			return proto.Gateway{}, false
		}
	case legacyGatewayDataTypeVersion:
		gw.Config.Log.Debug().
			Str("source", rec.Source.String()).
			Msg("Received legacy plaintext gateway payload")
	default:
		gw.Config.Log.Warn().
			Uint8("version", rec.Version).
			Str("source", rec.Source.String()).
			Msg("Dropping gateway data payload with unsupported version")
		return proto.Gateway{}, false
	}

	var gatewayData proto.Gateway
	if err := gatewayData.UnmarshalVT(decodedPayload); err != nil {
		gw.Config.Log.Error().Err(err).Msg("Error unmarshaling gateway data")
		return proto.Gateway{}, false
	}

	return gatewayData, true
}
