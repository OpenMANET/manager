package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/mdlayher/wifi"
	wificonfigv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/iwinfo"
	"github.com/openmanet/openmanetd/internal/mgmt"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

// WifiConfigService implements the wifi_configv1connect.WifiConfigServiceHandler.
type WifiConfigService struct {
	Log            zerolog.Logger
	IwinfoClient   iwinfo.IwinfoProvider
	Wifi           mgmt.WirelessProvider
	WirelessStatus network.WirelessStatusProvider
	ConfigReader   network.ConfigReader
	ParseBatHosts  func(string) (*batmanadv.BatHosts, error)
	DHCPLeases     LeaseProvider
	// GetMeshNeighbors can be overridden for testing; defaults to batmanadv.GetMeshNeighbors.
	GetMeshNeighbors func() (*batmanadv.Neighbors, error)

	mu sync.Mutex // serializes UCI writes
}

func (s *WifiConfigService) parseBatHosts(path string) (*batmanadv.BatHosts, error) {
	if s.ParseBatHosts != nil {
		return s.ParseBatHosts(path)
	}

	return batmanadv.ParseBatHostsFile(path)
}

func (s *WifiConfigService) getMeshNeighbors() (*batmanadv.Neighbors, error) {
	if s.GetMeshNeighbors != nil {
		return s.GetMeshNeighbors()
	}

	return batmanadv.GetMeshNeighbors()
}

// ListRadios returns all physical radios available on the device.
func (s *WifiConfigService) ListRadios(ctx context.Context, _ *emptypb.Empty) (*wificonfigv1.ListRadiosResponse, error) {
	s.Log.Debug().Msg("ListRadios request received")

	deviceSections, err := s.ConfigReader.GetSections(wirelessConfig, "wifi-device")
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list wifi-device sections: %w", err))
	}

	ifaceSections, err := s.ConfigReader.GetSections(wirelessConfig, "wifi-iface")
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list wifi-iface sections: %w", err))
	}

	// Get iwinfo data to correlate hardware names.
	var iwinfoData map[string]*iwinfo.InterfaceInfo
	if s.IwinfoClient != nil {
		iwinfoData, _ = s.IwinfoClient.GetInfoForAll(ctx)
	}

	// Get wireless status to map UCI radio→Linux ifname.
	var wsStatus map[string]*network.WirelessRadioStatus
	if s.WirelessStatus != nil {
		wsStatus, _ = s.WirelessStatus.GetWirelessStatus(ctx)
	}

	radios := make([]*wificonfigv1.Radio, 0, len(deviceSections))

	for _, devName := range deviceSections {
		dev, err := network.GetWirelessDeviceByNameWithReader(devName, s.ConfigReader)
		if err != nil {
			continue
		}

		// Find the first wifi-iface linked to this radio.
		ifaceName := findLinkedIface(devName, ifaceSections, s.ConfigReader)

		// Determine hardware name from iwinfo by matching ifname.
		hardwareName := resolveHardwareName(devName, wsStatus, iwinfoData)

		displayName := FormatBandDisplayName(WifiBandToProto(dev.Band))
		if hardwareName != "" {
			displayName += " (" + hardwareName + ")"
		}

		radios = append(radios, &wificonfigv1.Radio{
			Name:          devName,
			DisplayName:   displayName,
			HardwareName:  hardwareName,
			Band:          WifiBandToProto(dev.Band),
			InterfaceName: ifaceName,
		})
	}

	return &wificonfigv1.ListRadiosResponse{Radios: radios}, nil
}

// GetRadioStatus returns the current runtime status for a specific radio.
func (s *WifiConfigService) GetRadioStatus(ctx context.Context, req *wificonfigv1.GetRadioStatusRequest) (*wificonfigv1.GetRadioStatusResponse, error) {
	s.Log.Debug().Str("radio", req.GetRadioName()).Msg("GetRadioStatus request received")

	wsStatus, err := s.WirelessStatus.GetWirelessStatus(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("wireless status: %w", err))
	}

	radio, ok := wsStatus[req.GetRadioName()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("radio %q not found", req.GetRadioName()))
	}

	status := &wificonfigv1.RadioStatus{
		Active: radio.Up && !radio.Disabled,
	}

	if len(radio.Interfaces) == 0 {
		return &wificonfigv1.GetRadioStatusResponse{Status: status}, nil
	}

	ifname := radio.Interfaces[0].Ifname
	mode := radio.Interfaces[0].Config.Mode

	status.WifiMode = WifiModeToProto(mode)

	// Get iwinfo data for the interface.
	if s.IwinfoClient != nil {
		info, infoErr := s.IwinfoClient.GetInfo(ctx, ifname)
		if infoErr == nil && info != nil {
			status.Ssid = info.GetSSID()
			status.Mode = FormatModeDisplay(info.GetMode())
			status.Channel = int32(info.GetChannel())
			status.Frequency = int32(info.GetFrequency())
			status.Bandwidth = FormatBandwidthDisplay(info.GetHTMode())
			status.Encryption = FormatEncryptionDisplay(info.Encryption)
			status.TxPower = int32(info.GetTxPower())
		}
	}

	// Count connected stations.
	stationCount := s.countStations(ifname)
	if status.WifiMode == wificonfigv1.WifiMode_WIFI_MODE_AP {
		status.ConnectedClients = int32(stationCount)
	} else {
		status.MeshPeers = int32(stationCount)
	}

	return &wificonfigv1.GetRadioStatusResponse{Status: status}, nil
}

// GetRadioSettings returns the editable configuration for a specific radio.
func (s *WifiConfigService) GetRadioSettings(_ context.Context, req *wificonfigv1.GetRadioSettingsRequest) (*wificonfigv1.GetRadioSettingsResponse, error) {
	s.Log.Debug().Str("radio", req.GetRadioName()).Msg("GetRadioSettings request received")

	dev, err := network.GetWirelessDeviceByNameWithReader(req.GetRadioName(), s.ConfigReader)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read device config: %w", err))
	}

	ifaceSections, err := s.ConfigReader.GetSections(wirelessConfig, "wifi-iface")
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list wifi-iface sections: %w", err))
	}

	ifaceName := findLinkedIface(req.GetRadioName(), ifaceSections, s.ConfigReader)
	if ifaceName == "" {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no interface linked to radio %q", req.GetRadioName()))
	}

	iface, err := network.GetWirelessIfaceByNameWithReader(ifaceName, s.ConfigReader)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read iface config: %w", err))
	}

	txPower, _ := strconv.Atoi(dev.TxPower)

	settings := &wificonfigv1.RadioSettings{
		Ssid:       iface.SSID,
		Channel:    dev.Channel,
		Bandwidth:  WifiHTModeToProto(dev.HTMode),
		TxPower:    int32(txPower), //nolint:gosec // value originates from UCI config
		Encryption: WifiEncryptionToProto(iface.Encryption),
	}

	if iface.MeshID != "" {
		settings.MeshId = strPtr(iface.MeshID)
	}

	if dev.Country != "" {
		settings.Country = strPtr(dev.Country)
	}

	if iface.Disabled == "1" {
		settings.Disabled = boolPtr(true)
	}

	band := WifiBandToProto(dev.Band)

	// Populate available options for dropdowns.
	resp := &wificonfigv1.GetRadioSettingsResponse{
		Settings:              settings,
		AvailableChannels:     availableChannelsForBand(band),
		AvailableBandwidths:   availableBandwidthsForBand(band),
		AvailableEncryptions:  availableEncryptions(),
	}

	return resp, nil
}

// UpdateRadioSettings applies new configuration to a radio.
func (s *WifiConfigService) UpdateRadioSettings(_ context.Context, req *wificonfigv1.UpdateRadioSettingsRequest) (*wificonfigv1.UpdateRadioSettingsResponse, error) {
	s.Log.Debug().Str("radio", req.GetRadioName()).Msg("UpdateRadioSettings request received")

	s.mu.Lock()
	defer s.mu.Unlock()

	settings := req.GetSettings()
	if settings == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("settings are required"))
	}

	radioName := req.GetRadioName()

	// Find the linked interface section.
	ifaceSections, err := s.ConfigReader.GetSections(wirelessConfig, "wifi-iface")
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list wifi-iface sections: %w", err))
	}

	ifaceName := findLinkedIface(radioName, ifaceSections, s.ConfigReader)
	if ifaceName == "" {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no interface linked to radio %q", radioName))
	}

	// Build device config update.
	devCfg := &network.UCIWirelessDevice{
		Channel: settings.GetChannel(),
		TxPower: strconv.Itoa(int(settings.GetTxPower())),
	}

	if htmode := ProtoToWifiHTMode(settings.GetBandwidth()); htmode != "" {
		devCfg.HTMode = htmode
	}

	if settings.Country != nil {
		devCfg.Country = settings.GetCountry()
	}

	if err := network.SetWirelessDeviceConfigWithReader(radioName, devCfg, s.ConfigReader); err != nil {
		return &wificonfigv1.UpdateRadioSettingsResponse{
			Success: false,
			Message: strPtr(fmt.Sprintf("failed to update device config: %v", err)),
		}, nil
	}

	// Build interface config update.
	ifaceCfg := &network.UCIWirelessIface{
		SSID: settings.GetSsid(),
	}

	if settings.Password != nil && settings.GetPassword() != "" {
		ifaceCfg.Key = settings.GetPassword()
	}

	if settings.MeshId != nil {
		ifaceCfg.MeshID = settings.GetMeshId()
	}

	if enc := ProtoToWifiEncryption(settings.GetEncryption()); enc != "" {
		ifaceCfg.Encryption = enc
	}

	if settings.Disabled != nil {
		if settings.GetDisabled() {
			ifaceCfg.Disabled = "1"
		} else {
			ifaceCfg.Disabled = "0"
		}
	}

	if err := network.SetWirelessIfaceConfigWithReader(ifaceName, ifaceCfg, s.ConfigReader); err != nil {
		return &wificonfigv1.UpdateRadioSettingsResponse{
			Success: false,
			Message: strPtr(fmt.Sprintf("failed to update iface config: %v", err)),
		}, nil
	}

	// Reload the wireless subsystem so changes take effect.
	if err := s.ConfigReader.ReloadConfig(); err != nil {
		return &wificonfigv1.UpdateRadioSettingsResponse{
			Success: false,
			Message: strPtr(fmt.Sprintf("config committed but reload failed: %v", err)),
		}, nil
	}

	return &wificonfigv1.UpdateRadioSettingsResponse{Success: true}, nil
}

// ListConnectedClients returns all clients connected to an AP-mode radio.
func (s *WifiConfigService) ListConnectedClients(ctx context.Context, req *wificonfigv1.ListConnectedClientsRequest) (*wificonfigv1.ListConnectedClientsResponse, error) {
	s.Log.Debug().Str("radio", req.GetRadioName()).Msg("ListConnectedClients request received")

	ifname, err := s.resolveIfname(ctx, req.GetRadioName())
	if err != nil {
		return nil, err
	}

	iface := s.findWifiInterface(ifname)
	if iface == nil {
		return &wificonfigv1.ListConnectedClientsResponse{}, nil
	}

	stations, err := s.Wifi.StationInfo(iface)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("station info: %w", err))
	}

	// Build MAC→hostname map from DHCP leases.
	macToHostname := s.buildMACHostnameMap(ctx)

	clients := make([]*wificonfigv1.ConnectedClient, 0, len(stations))

	for _, st := range stations {
		mac := st.HardwareAddr.String()

		clients = append(clients, &wificonfigv1.ConnectedClient{
			Hostname:  macToHostname[strings.ToLower(mac)],
			MacAddress: mac,
			SignalDbm: int32(st.Signal),
			RxRateBps: int64(st.ReceiveBitrate),
			TxRateBps: int64(st.TransmitBitrate),
			Connected: durationpb.New(st.Connected),
		})
	}

	return &wificonfigv1.ListConnectedClientsResponse{Clients: clients}, nil
}

// ListMeshPeers returns all mesh peers visible to a mesh-mode radio.
func (s *WifiConfigService) ListMeshPeers(ctx context.Context, req *wificonfigv1.ListMeshPeersRequest) (*wificonfigv1.ListMeshPeersResponse, error) {
	s.Log.Debug().Str("radio", req.GetRadioName()).Msg("ListMeshPeers request received")

	ifname, err := s.resolveIfname(ctx, req.GetRadioName())
	if err != nil {
		return nil, err
	}

	// Get bat-hosts for MAC→hostname mapping.
	batHosts, batErr := s.parseBatHosts(batmanadv.BatHostsFilePath)
	if batErr != nil {
		s.Log.Warn().Err(batErr).Msg("Failed to parse bat-hosts; hostnames will be empty")

		batHosts = &batmanadv.BatHosts{}
	}

	// Get batman-adv neighbors for throughput/last_seen.
	batNeighbors, batNErr := s.getMeshNeighbors()
	if batNErr != nil {
		s.Log.Warn().Err(batNErr).Msg("Failed to get batman-adv neighbors; throughput/last_seen unavailable")
	}

	// Build peer list, preferring batman-adv neighbor list as the primary source.
	peers := s.buildMeshPeerList(ifname, batHosts, batNeighbors)

	return &wificonfigv1.ListMeshPeersResponse{Peers: peers}, nil
}

// ── internal helpers ─────────────────────────────────────────────────────────

const wirelessConfig = "wireless"

// findLinkedIface returns the first wifi-iface section whose "device" option
// matches the given radio device name.
func findLinkedIface(deviceName string, ifaceSections []string, reader network.ConfigReader) string {
	for _, sec := range ifaceSections {
		vals, ok := reader.Get(wirelessConfig, sec, "device")
		if ok && len(vals) > 0 && vals[0] == deviceName {
			return sec
		}
	}

	return ""
}

// resolveHardwareName looks up the hardware chip name by correlating the UCI
// radio to its running Linux interface name, then matching that to iwinfo data.
func resolveHardwareName(radioName string, wsStatus map[string]*network.WirelessRadioStatus,
	iwinfoData map[string]*iwinfo.InterfaceInfo) string {
	if wsStatus == nil || iwinfoData == nil {
		return ""
	}

	rs, ok := wsStatus[radioName]
	if !ok || len(rs.Interfaces) == 0 {
		return ""
	}

	ifname := rs.Interfaces[0].Ifname

	info, ok := iwinfoData[ifname]
	if !ok || info == nil {
		return ""
	}

	return info.GetHardwareName()
}

// resolveIfname maps a UCI radio name to the running Linux interface name.
func (s *WifiConfigService) resolveIfname(ctx context.Context, radioName string) (string, error) {
	wsStatus, err := s.WirelessStatus.GetWirelessStatus(ctx)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("wireless status: %w", err))
	}

	radio, ok := wsStatus[radioName]
	if !ok || len(radio.Interfaces) == 0 {
		return "", connect.NewError(connect.CodeNotFound, fmt.Errorf("radio %q not found or has no interfaces", radioName))
	}

	return radio.Interfaces[0].Ifname, nil
}

// findWifiInterface finds a wifi.Interface by name from the Wifi provider.
func (s *WifiConfigService) findWifiInterface(ifname string) *wifi.Interface {
	ifaces, err := s.Wifi.Interfaces()
	if err != nil {
		return nil
	}

	for _, iface := range ifaces {
		if iface.Name == ifname {
			return iface
		}
	}

	return nil
}

// countStations returns the number of stations connected to the named interface.
func (s *WifiConfigService) countStations(ifname string) int {
	iface := s.findWifiInterface(ifname)
	if iface == nil {
		return 0
	}

	stations, err := s.Wifi.StationInfo(iface)
	if err != nil {
		return 0
	}

	return len(stations)
}

// buildMACHostnameMap creates a lowercase-MAC → hostname map from DHCP leases.
func (s *WifiConfigService) buildMACHostnameMap(ctx context.Context) map[string]string {
	m := map[string]string{}

	if s.DHCPLeases == nil {
		return m
	}

	resp, err := s.DHCPLeases.GetCurrentDHCPLeases(ctx)
	if err != nil {
		return m
	}

	for _, l := range resp.GetDHCPLeases() {
		m[strings.ToLower(l.MacAddr)] = l.Hostname
	}

	return m
}

// buildMeshPeerList assembles the protobuf MeshPeer list.
func (s *WifiConfigService) buildMeshPeerList(
	ifname string,
	batHosts *batmanadv.BatHosts,
	batNeighbors *batmanadv.Neighbors,
) []*wificonfigv1.MeshPeer {
	if batNeighbors == nil || batNeighbors.IsEmpty() {
		return nil
	}

	// Filter neighbors to the interface we care about.
	filtered := batNeighbors.FilterByInterface(ifname)

	// Also get station info for signal enrichment.
	signalByMAC := map[string]int{}
	iface := s.findWifiInterface(ifname)

	if iface != nil {
		stInfos, err := s.Wifi.StationInfo(iface)
		if err == nil {
			for _, st := range stInfos {
				signalByMAC[strings.ToLower(st.HardwareAddr.String())] = st.Signal
			}
		}
	}

	peers := make([]*wificonfigv1.MeshPeer, 0, len(filtered))

	for _, n := range filtered {
		mac := n.NeighAddress
		hostname := batHosts.GetHostByMAC(mac)

		peer := &wificonfigv1.MeshPeer{
			Hostname:      hostname,
			MacAddress:    mac,
			SignalDbm:     int32(signalByMAC[strings.ToLower(mac)]),
			ThroughputMbps: float64(n.Throughput) / 10.0, // batman-adv reports in 100kbit/s
			LastSeen:      durationpb.New(time.Duration(n.LastSeenMsecs) * time.Millisecond),
		}

		peers = append(peers, peer)
	}

	return peers
}

// ── formatting helpers ───────────────────────────────────────────────────────

// FormatEncryptionDisplay converts iwinfo EncryptionInfo to a display string.
func FormatEncryptionDisplay(enc iwinfo.EncryptionInfo) string {
	if !enc.IsEnabled() {
		return "Open"
	}

	wpa := enc.GetWPA()
	auth := enc.GetAuthentication()
	ciphers := enc.GetCiphers()

	if len(wpa) == 0 || len(auth) == 0 {
		return "Encrypted"
	}

	// Use the highest WPA version.
	maxWPA := wpa[0]
	for _, v := range wpa {
		if v > maxWPA {
			maxWPA = v
		}
	}

	var prefix string

	switch maxWPA {
	case 3:
		prefix = "WPA3"
	case 2:
		prefix = "WPA2"
	case 1:
		prefix = "WPA"
	default:
		prefix = fmt.Sprintf("WPA%d", maxWPA)
	}

	authStr := strings.ToUpper(auth[0])
	if authStr == "SAE" {
		return prefix + "-SAE"
	}

	result := prefix + "-" + authStr
	if len(ciphers) > 0 {
		result += " (" + strings.ToUpper(ciphers[0]) + ")"
	}

	return result
}

// FormatBandwidthDisplay converts an HTMode string to human-readable bandwidth.
func FormatBandwidthDisplay(htmode string) string {
	htmode = strings.ToUpper(htmode)

	bandwidthMap := map[string]string{
		"NOHT":   "No HT",
		"HT20":   "20 MHz",
		"HT40":   "40 MHz",
		"VHT20":  "20 MHz",
		"VHT40":  "40 MHz",
		"VHT80":  "80 MHz",
		"VHT160": "160 MHz",
		"HE20":   "20 MHz",
		"HE40":   "40 MHz",
		"HE80":   "80 MHz",
		"HE160":  "160 MHz",
	}

	if bw, ok := bandwidthMap[htmode]; ok {
		return bw
	}

	// S1G bandwidths.
	if strings.HasSuffix(htmode, "MHZ") || strings.HasSuffix(htmode, "MHz") {
		return htmode
	}

	if htmode == "" {
		return ""
	}

	return htmode
}

// FormatModeDisplay converts iwinfo mode string to user-friendly display.
func FormatModeDisplay(mode string) string {
	switch strings.ToLower(mode) {
	case "master":
		return "Access Point"
	case "mesh point":
		return "Mesh Point (802.11s)"
	case "client":
		return "Client"
	case "ad-hoc":
		return "Ad-Hoc"
	case "monitor":
		return "Monitor"
	default:
		return mode
	}
}

// FormatBandDisplayName converts a WifiBand enum to a display name.
func FormatBandDisplayName(band wificonfigv1.WifiBand) string {
	switch band {
	case wificonfigv1.WifiBand_WIFI_BAND_2G:
		return "2.4 GHz Radio"
	case wificonfigv1.WifiBand_WIFI_BAND_5G:
		return "5 GHz Radio"
	case wificonfigv1.WifiBand_WIFI_BAND_6G:
		return "6 GHz Radio"
	case wificonfigv1.WifiBand_WIFI_BAND_S1G:
		return "HaLow Radio"
	case wificonfigv1.WifiBand_WIFI_BAND_60G:
		return "60 GHz Radio"
	default:
		return band.String() + " Radio"
	}
}

// ── option lists ─────────────────────────────────────────────────────────────

func availableChannelsForBand(band wificonfigv1.WifiBand) []string {
	switch band {
	case wificonfigv1.WifiBand_WIFI_BAND_2G:
		return []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}
	case wificonfigv1.WifiBand_WIFI_BAND_5G:
		return []string{"36", "40", "44", "48", "52", "56", "60", "64",
			"100", "104", "108", "112", "116", "120", "124", "128",
			"132", "136", "140", "144", "149", "153", "157", "161", "165"}
	case wificonfigv1.WifiBand_WIFI_BAND_6G:
		return []string{"1", "5", "9", "13", "17", "21", "25", "29",
			"33", "37", "41", "45", "49", "53", "57", "61",
			"65", "69", "73", "77", "81", "85", "89", "93"}
	case wificonfigv1.WifiBand_WIFI_BAND_S1G:
		return []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
			"11", "12", "13", "14", "15", "16", "17", "18", "19", "20",
			"21", "22", "23", "24", "25", "26", "27", "28", "29", "30",
			"31", "32", "33", "34", "35", "36", "37", "38", "39", "40",
			"41", "42", "43", "44", "45", "46", "47", "48", "49", "50",
			"51"}
	default:
		return nil
	}
}

func availableBandwidthsForBand(band wificonfigv1.WifiBand) []wificonfigv1.WifiHTMode {
	switch band {
	case wificonfigv1.WifiBand_WIFI_BAND_2G:
		return []wificonfigv1.WifiHTMode{
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_NOHT,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT20,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT40,
		}
	case wificonfigv1.WifiBand_WIFI_BAND_5G:
		return []wificonfigv1.WifiHTMode{
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT20,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT40,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT80,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT160,
		}
	case wificonfigv1.WifiBand_WIFI_BAND_6G:
		return []wificonfigv1.WifiHTMode{
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE20,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE40,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE80,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE160,
		}
	case wificonfigv1.WifiBand_WIFI_BAND_S1G:
		return []wificonfigv1.WifiHTMode{
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_1MHZ,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_2MHZ,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_4MHZ,
			wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_8MHZ,
		}
	default:
		return nil
	}
}

func availableEncryptions() []wificonfigv1.WifiEncryption {
	return []wificonfigv1.WifiEncryption{
		wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_SAE,
		wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK2,
		wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK,
		wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_NONE,
	}
}

// ── enum conversion helpers ──────────────────────────────────────────────────

// WifiBandToProto converts a UCI band string to the WifiBand enum.
func WifiBandToProto(s string) wificonfigv1.WifiBand {
	switch strings.ToLower(s) {
	case "2g":
		return wificonfigv1.WifiBand_WIFI_BAND_2G
	case "5g":
		return wificonfigv1.WifiBand_WIFI_BAND_5G
	case "6g":
		return wificonfigv1.WifiBand_WIFI_BAND_6G
	case "s1g":
		return wificonfigv1.WifiBand_WIFI_BAND_S1G
	case "60g":
		return wificonfigv1.WifiBand_WIFI_BAND_60G
	default:
		return wificonfigv1.WifiBand_WIFI_BAND_UNSPECIFIED
	}
}

// WifiModeToProto converts an iwinfo/UCI mode string to the WifiMode enum.
func WifiModeToProto(s string) wificonfigv1.WifiMode {
	switch strings.ToLower(s) {
	case "ap", "master":
		return wificonfigv1.WifiMode_WIFI_MODE_AP
	case "mesh", "mesh point":
		return wificonfigv1.WifiMode_WIFI_MODE_MESH
	case "sta", "client", "managed":
		return wificonfigv1.WifiMode_WIFI_MODE_STA
	case "adhoc", "ad-hoc", "ibss":
		return wificonfigv1.WifiMode_WIFI_MODE_ADHOC
	case "monitor":
		return wificonfigv1.WifiMode_WIFI_MODE_MONITOR
	default:
		return wificonfigv1.WifiMode_WIFI_MODE_UNSPECIFIED
	}
}

// WifiEncryptionToProto converts a UCI encryption string to the WifiEncryption enum.
func WifiEncryptionToProto(s string) wificonfigv1.WifiEncryption {
	switch strings.ToLower(s) {
	case "sae":
		return wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_SAE
	case "psk2":
		return wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK2
	case "psk":
		return wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK
	case "psk-mixed":
		return wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK_MIXED
	case "none":
		return wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_NONE
	case "owe":
		return wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_OWE
	default:
		return wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_UNSPECIFIED
	}
}

// ProtoToWifiEncryption converts a WifiEncryption enum to the UCI string.
func ProtoToWifiEncryption(e wificonfigv1.WifiEncryption) string {
	switch e {
	case wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_SAE:
		return "sae"
	case wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK2:
		return "psk2"
	case wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK:
		return "psk"
	case wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK_MIXED:
		return "psk-mixed"
	case wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_NONE:
		return "none"
	case wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_OWE:
		return "owe"
	default:
		return ""
	}
}

// WifiHTModeToProto converts a UCI htmode string to the WifiHTMode enum.
func WifiHTModeToProto(s string) wificonfigv1.WifiHTMode {
	switch strings.ToUpper(s) {
	case "NOHT":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_NOHT
	case "HT20":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT20
	case "HT40-":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT40_MINUS
	case "HT40+":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT40_PLUS
	case "HT40":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT40
	case "VHT20":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT20
	case "VHT40":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT40
	case "VHT80":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT80
	case "VHT160":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT160
	case "HE20":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE20
	case "HE40":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE40
	case "HE80":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE80
	case "HE160":
		return wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE160
	default:
		// S1G bandwidth values use lowercase with spaces.
		switch strings.ToLower(s) {
		case "1 mhz":
			return wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_1MHZ
		case "2 mhz":
			return wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_2MHZ
		case "4 mhz":
			return wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_4MHZ
		case "8 mhz":
			return wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_8MHZ
		default:
			return wificonfigv1.WifiHTMode_WIFI_HT_MODE_UNSPECIFIED
		}
	}
}

// ProtoToWifiHTMode converts a WifiHTMode enum to the UCI string.
func ProtoToWifiHTMode(h wificonfigv1.WifiHTMode) string {
	switch h {
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_NOHT:
		return "NOHT"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT20:
		return "HT20"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT40_MINUS:
		return "HT40-"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT40_PLUS:
		return "HT40+"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_HT40:
		return "HT40"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT20:
		return "VHT20"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT40:
		return "VHT40"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT80:
		return "VHT80"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_VHT160:
		return "VHT160"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE20:
		return "HE20"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE40:
		return "HE40"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE80:
		return "HE80"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_HE160:
		return "HE160"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_1MHZ:
		return "1 MHz"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_2MHZ:
		return "2 MHz"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_4MHZ:
		return "4 MHz"
	case wificonfigv1.WifiHTMode_WIFI_HT_MODE_S1G_8MHZ:
		return "8 MHz"
	default:
		return ""
	}
}

// ── pointer helpers ──────────────────────────────────────────────────────────

func strPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}
