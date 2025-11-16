package mgmt

import (
	"net"
	"os"
	"time"

	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	GatewayDataType        uint8 = 100
	GatewayDataTypeVersion uint8 = 1
)

type GatewayWorker struct {
	Config       ManagementConfig
	Client       *alfred.Client
	ShutdownChan <-chan any

	sendInterval time.Duration
	recvInterval time.Duration
}

func NewGatewayWorker(config *ManagementConfig, client *alfred.Client, shutdownChan <-chan any) *GatewayWorker {
	config.Log.Info().Msg("GatewayWorker initialized")

	return &GatewayWorker{
		Config:       *config,
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

				err = gw.Client.Set(GatewayDataType, GatewayDataTypeVersion, gatewayDataBytes)
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
			} else {
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
				// gateway records and match batman-adv router MAC to the received gateway MAC
				// This is to identify the active gateway in the mesh
				if len(*batGwys) == 1 {
					batGw := batGwys.GetBest()
					for _, rec := range record {
						var gatewayData proto.Gateway
						err = gatewayData.UnmarshalVT(rec.Data)

						if err != nil {
							gw.Config.Log.Error().Err(err).Msg("Error unmarshaling gateway data")
							continue
						}

						if gatewayData.Mac == batGw.Router {
							gw.Config.Log.Debug().Msgf("Matched gateway: %+v", &gatewayData)

							// Replace default route with the matched gateway IP
							ipString := net.ParseIP(gatewayData.Ipaddr)
							if ipString != nil {
								if err := network.ReplaceDefaultRoute(ipString); err != nil {
									gw.Config.Log.Error().Err(err).Msgf("Failed to replace default route with gateway %s", gatewayData.Ipaddr)
								}
							}

						}
					}
					// Skip further processing as we have already matched the single gateway
					continue
				}

				if len(*batGwys) > 1 {
					gw.Config.Log.Debug().Msg("Multiple gateways present in batman-adv")
					// Process received gateway records
					for _, rec := range record {
						// Unmarshal gateway data
						var gatewayData proto.Gateway
						err = gatewayData.UnmarshalVT(rec.Data)
						if err != nil {
							gw.Config.Log.Error().Err(err).Msg("Error unmarshaling gateway data")
							continue
						}

						// Check if this gateway is present in batman-adv
					}
				}
			}
		}
	}
}
