package mgmt

import (
	"fmt"
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
	GatewayDataType        uint8 = uint8(proto.DataType_DATA_TYPE_GATEWAY)
	GatewayDataTypeVersion uint8 = 1
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
func (gw *GatewayWorker) StartSend() { //nolint:gocognit
	ticker := time.NewTicker(gw.sendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gw.ShutdownChan:
			return
		case <-ticker.C:
			if err := gw.sendGatewayDataOnce(gw.Client); err != nil {
				gw.Config.Log.Error().Err(err).Msg("Error in gateway data send tick")
			}
		}
	}
}

// sendGatewayDataOnce performs a single iteration of the gateway data send logic.
func (gw *GatewayWorker) sendGatewayDataOnce(client alfredClient) error {
	return gw.sendGatewayDataOnceWithDeps(client, gw.Config.uciOpenMANETConfig,
		batmanadv.GetMeshConfig, network.GetInterfaceByName, os.Hostname)
}

// gatewayMeshConfigGetter abstracts how we retrieve mesh configuration.
type gatewayMeshConfigGetter func(batIface string) (*batmanadv.MeshConfig, error)

// sendGatewayDataOnceWithDeps is the dependency-injectable version of sendGatewayDataOnce.
func (gw *GatewayWorker) sendGatewayDataOnceWithDeps(
	client alfredClient,
	openMANETReader network.OpenMANETConfigReader,
	getMeshConfig gatewayMeshConfigGetter,
	getIface func(string) network.NetworkInterface,
	getHostname func() (string, error),
) error {
	configured, err := network.IsDHCPConfiguredWithReader(openMANETReader)
	if err != nil {
		return fmt.Errorf("check DHCP configuration: %w", err)
	}

	if !configured {
		gw.Config.Log.Debug().Msg("Static Address & DHCP not configured, skipping gateway data send")

		return nil
	}

	// Get mesh config from batman-adv to check if we are in gateway mode
	meshCfg, err := getMeshConfig(gw.Config.BatInterface)
	if err != nil {
		return fmt.Errorf("get mesh config: %w", err)
	}

	// Only send gateway data if we are in gateway mode
	if !meshCfg.IsGatewayMode() {
		return nil
	}

	iface := getIface(gw.Config.IFace)

	hostname, err := getHostname()
	if err != nil {
		gw.Config.Log.Error().Err(err).Msg("Error getting hostname")

		hostname = "unknown"
	}

	// Verify that the interface has an IP address
	if len(iface.IP) == 0 {
		return fmt.Errorf("interface %s has no IP address", gw.Config.IFace)
	}

	// Verify that the interface has a valid IPV4 address
	if iface.IP[0].IP.To4() == nil {
		return fmt.Errorf("interface %s has no valid IPv4 address", gw.Config.IFace)
	}

	// Prepare gateway data
	gatewayData := proto.Gateway{
		Mac:      meshCfg.HardAddress,
		Ipaddr:   iface.IP[0].IP.String(),
		Hostname: hostname,
	}

	gatewayDataBytes, err := gatewayData.MarshalVT()
	if err != nil {
		return fmt.Errorf("marshal gateway data: %w", err)
	}

	if err := client.Set(GatewayDataType, GatewayDataTypeVersion, gatewayDataBytes); err != nil {
		return fmt.Errorf("send gateway data: %w", err)
	}

	return nil
}

// Start begins the periodic receiving of gateway data from the Alfred client.
func (gw *GatewayWorker) StartReceive() { //nolint:gocognit
	ticker := time.NewTicker(gw.recvInterval)
	defer ticker.Stop()

	for {
		select {
		case <-gw.ShutdownChan:
			return
		case <-ticker.C:
			if err := gw.receiveGatewayDataOnce(gw.Client); err != nil {
				gw.Config.Log.Error().Err(err).Msg("Error in gateway data receive tick")
			}
		}
	}
}

// gatewayMeshGatewaysGetter abstracts how we retrieve mesh gateways.
type gatewayMeshGatewaysGetter func(batIface string) (*batmanadv.Gateways, error)

// routeReplacer abstracts the default route replacement function.
type routeReplacer func(newGateway net.IP, iface string) error

// dnsSetterFn abstracts the DNS setting function.
type dnsSetterFn func(section, dns string, reader network.ConfigReader) error

// receiveGatewayDataOnce performs a single iteration of the gateway data receive logic.
func (gw *GatewayWorker) receiveGatewayDataOnce(client alfredClient) error {
	return gw.receiveGatewayDataOnceWithDeps(client, batmanadv.GetMeshConfig, batmanadv.GetMeshGateways,
		network.ReplaceDefaultRoute, network.SetNetworkDNSWithReader, gw.Config.uciNetworkConfig)
}

// receiveGatewayDataOnceWithDeps is the dependency-injectable version of receiveGatewayDataOnce.
func (gw *GatewayWorker) receiveGatewayDataOnceWithDeps( //nolint:gocognit
	client alfredClient,
	getMeshConfig gatewayMeshConfigGetter,
	getMeshGateways gatewayMeshGatewaysGetter,
	replaceRoute routeReplacer,
	setDNS dnsSetterFn,
	networkReader network.ConfigReader,
) error {
	// If we are not in gateway mode, process received gateway data
	meshCfg, err := getMeshConfig(gw.Config.BatInterface)
	if err != nil {
		return fmt.Errorf("get mesh config: %w", err)
	}

	if meshCfg.IsGatewayMode() {
		// Skip processing if we are in gateway mode
		return nil
	}

	records, err := client.Request(GatewayDataType)
	if err != nil {
		return fmt.Errorf("request gateway data: %w", err)
	}

	// Get the gateway status from batman-adv
	batGwys, err := getMeshGateways(gw.Config.BatInterface)
	if err != nil {
		return fmt.Errorf("get mesh gateways: %w", err)
	}

	// If no gateways are present in batman-adv, skip processing
	if len(*batGwys) == 0 {
		gw.Config.Log.Debug().Msg("No gateways present in batman-adv")

		return nil
	}

	batGw := batGwys.GetBest()

	for _, rec := range records {
		var gatewayData proto.Gateway

		if err := gatewayData.UnmarshalVT(rec.Data); err != nil {
			gw.Config.Log.Error().Err(err).Msg("Error unmarshaling gateway data")

			continue
		}

		if gatewayData.Mac != batGw.OrigAddress {
			continue
		}

		// Replace default route with the matched gateway IP
		ipString := net.ParseIP(gatewayData.Ipaddr)
		if ipString == nil {
			continue
		}

		if routeErr := replaceRoute(ipString, gw.Config.IFace); routeErr != nil {
			gw.Config.Log.Error().Err(routeErr).Msgf("Failed to replace default route with gateway %s", gatewayData.Ipaddr)
		}

		iFaceName, ifaceErr := util.InterfaceWithoutBridge(gw.Config.IFace)
		if ifaceErr != nil {
			gw.Config.Log.Error().Err(ifaceErr).Msg("Error normalizing interface name for DNS setting")

			continue
		}

		if dnsErr := setDNS(iFaceName, ipString.String(), networkReader); dnsErr != nil {
			gw.Config.Log.Error().Err(dnsErr).Msgf("Failed to set DNS server to gateway %s", gatewayData.Ipaddr)
		}

		// Once we've matched the best gateway, stop processing
		break
	}

	return nil
}
