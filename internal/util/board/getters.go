package board

// Board getters

// GetModel returns the Model information from the Board.
func (b *Board) GetModel() Model {
	return b.Model
}

// GetSystem returns the System information from the Board.
func (b *Board) GetSystem() System {
	return b.System
}

// GetNetwork returns the Network configuration from the Board.
func (b *Board) GetNetwork() Network {
	return b.Network
}

// Model getters

// GetID returns the model ID string.
func (m *Model) GetID() string {
	return m.ID
}

// GetName returns the model name string.
func (m *Model) GetName() string {
	return m.Name
}

// System getters

// GetHostname returns the system hostname.
func (s *System) GetHostname() string {
	return s.Hostname
}

// Network getters

// GetLan returns the LAN interface configuration.
func (n *Network) GetLan() Lan {
	return n.Lan
}

// Lan getters

// GetDevice returns the network device name (e.g., "eth0", "wlan0").
func (l *Lan) GetDevice() string {
	return l.Device
}

// GetProtocol returns the network protocol (e.g., "static", "dhcp").
func (l *Lan) GetProtocol() string {
	return l.Protocol
}

// GetIpaddr returns the IP address assigned to the LAN interface.
func (l *Lan) GetIpaddr() string {
	return l.Ipaddr
}

// GetNetmask returns the network mask for the LAN interface.
func (l *Lan) GetNetmask() string {
	return l.Netmask
}

// Wlan getters - Phy0

// GetPhy0Path returns the hardware path for the Phy0 wireless interface.
func (b *Board) GetPhy0Path() string {
	return b.Wlan.Phy0.Path
}

// GetPhy0AntennaRx returns the number of receive antennas for Phy0.
func (b *Board) GetPhy0AntennaRx() int {
	return b.Wlan.Phy0.Info.AntennaRx
}

// GetPhy0AntennaTx returns the number of transmit antennas for Phy0.
func (b *Board) GetPhy0AntennaTx() int {
	return b.Wlan.Phy0.Info.AntennaTx
}

// GetPhy0Bands returns the supported frequency bands for Phy0.
func (b *Board) GetPhy0Bands() Bands {
	return b.Wlan.Phy0.Info.Bands
}

// GetPhy0Radios returns the list of radios associated with the phy0 wireless interface.
// The returned slice contains radio interface information as generic types.
func (b *Board) GetPhy0Radios() []interface{} {
	return b.Wlan.Phy0.Info.Radios
}

// Wlan getters - Phy1

// GetPhy1Path returns the hardware path for the Phy1 wireless interface.
func (b *Board) GetPhy1Path() string {
	return b.Wlan.Phy1.Path
}

// GetPhy1AntennaRx returns the number of receive antennas for Phy1.
func (b *Board) GetPhy1AntennaRx() int {
	return b.Wlan.Phy1.Info.AntennaRx
}

// GetPhy1AntennaTx returns the number of transmit antennas for Phy1.
func (b *Board) GetPhy1AntennaTx() int {
	return b.Wlan.Phy1.Info.AntennaTx
}

// GetPhy1Bands returns the supported frequency bands for Phy1.
func (b *Board) GetPhy1Bands() Bands {
	return b.Wlan.Phy1.Info.Bands
}

// GetPhy1Radios returns the list of radio configurations for Phy1.
func (b *Board) GetPhy1Radios() []interface{} {
	return b.Wlan.Phy1.Info.Radios
}

// Wlan getters - Phy2

// GetPhy2Path returns the hardware path for the Phy2 wireless interface.
func (b *Board) GetPhy2Path() string {
	return b.Wlan.Phy2.Path
}

// GetPhy2AntennaRx returns the number of receive antennas for Phy2.
func (b *Board) GetPhy2AntennaRx() int {
	return b.Wlan.Phy2.Info.AntennaRx
}

// GetPhy2AntennaTx returns the number of transmit antennas for Phy2.
func (b *Board) GetPhy2AntennaTx() int {
	return b.Wlan.Phy2.Info.AntennaTx
}

// GetPhy2Bands returns the supported frequency bands for Phy2.
func (b *Board) GetPhy2Bands() Bands {
	return b.Wlan.Phy2.Info.Bands
}

// GetPhy2Radios returns the list of radio configurations for Phy2.
func (b *Board) GetPhy2Radios() []interface{} {
	return b.Wlan.Phy2.Info.Radios
}

// Bands getters - 2G

// Get2GHt returns whether HT (High Throughput) is supported on the 2.4 GHz band.
func (b *Bands) Get2GHt() bool {
	return b.TwoG.Ht
}

// Get2GHe returns whether HE (High Efficiency/Wi-Fi 6) is supported on the 2.4 GHz band.
func (b *Bands) Get2GHe() bool {
	return b.TwoG.He
}

// Get2GMaxWidth returns the maximum channel width in MHz for the 2.4 GHz band.
func (b *Bands) Get2GMaxWidth() int {
	return b.TwoG.MaxWidth
}

// Get2GModes returns the list of supported modes for the 2.4 GHz band (e.g., "NOHT", "HT20", "HT40").
func (b *Bands) Get2GModes() []string {
	return b.TwoG.Modes
}

// Get2GDefaultChannel returns the default channel number for the 2.4 GHz band.
func (b *Bands) Get2GDefaultChannel() int {
	return b.TwoG.DefaultChannel
}

// Bands getters - 5G

// Get5GHt returns whether HT (High Throughput) is supported on the 5 GHz band.
func (b *Bands) Get5GHt() bool {
	return b.FiveG.Ht
}

// Get5GVht returns whether VHT (Very High Throughput/Wi-Fi 5) is supported on the 5 GHz band.
func (b *Bands) Get5GVht() bool {
	return b.FiveG.Vht
}

// Get5GHe returns whether HE (High Efficiency/Wi-Fi 6) is supported on the 5 GHz band.
func (b *Bands) Get5GHe() bool {
	return b.FiveG.He
}

// Get5GMaxWidth returns the maximum channel width in MHz for the 5 GHz band.
func (b *Bands) Get5GMaxWidth() int {
	return b.FiveG.MaxWidth
}

// Get5GModes returns the list of supported modes for the 5 GHz band (e.g., "VHT80", "VHT160").
func (b *Bands) Get5GModes() []string {
	return b.FiveG.Modes
}

// Get5GDefaultChannel returns the default channel number for the 5 GHz band.
func (b *Bands) Get5GDefaultChannel() int {
	return b.FiveG.DefaultChannel
}

// Bands getters - 6G

// Get6GHe returns whether HE (High Efficiency/Wi-Fi 6E) is supported on the 6 GHz band.
func (b *Bands) Get6GHe() bool {
	return b.SixG.He
}

// Get6GMaxWidth returns the maximum channel width in MHz for the 6 GHz band.
func (b *Bands) Get6GMaxWidth() int {
	return b.SixG.MaxWidth
}

// Get6GModes returns the list of supported modes for the 6 GHz band (e.g., "HE80", "HE160").
func (b *Bands) Get6GModes() []string {
	return b.SixG.Modes
}

// Get6GDefaultChannel returns the default channel number for the 6 GHz band.
func (b *Bands) Get6GDefaultChannel() int {
	return b.SixG.DefaultChannel
}
