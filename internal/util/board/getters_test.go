package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadTestBoard(t *testing.T) *Board {
	boardPath := filepath.Join("..", "..", "..", "testfixtures", "uci", "board.json")
	data, err := os.ReadFile(boardPath)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	var board Board
	if err := json.Unmarshal(data, &board); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	return &board
}

func TestBoardGetters(t *testing.T) {
	board := loadTestBoard(t)

	// Test Board getters
	model := board.GetModel()
	if model.GetID() != "bcm2711,mm6108-spi" {
		t.Errorf("Expected model ID 'bcm2711,mm6108-spi', got '%s'", model.GetID())
	}
	if model.GetName() != "RPI RPI4-MM6108 (SPI)" {
		t.Errorf("Expected model name 'RPI RPI4-MM6108 (SPI)', got '%s'", model.GetName())
	}

	system := board.GetSystem()
	if system.GetHostname() != "BCM2711-88ba" {
		t.Errorf("Expected hostname 'BCM2711-88ba', got '%s'", system.GetHostname())
	}

	network := board.GetNetwork()
	lan := network.GetLan()
	if lan.GetDevice() != "eth0" {
		t.Errorf("Expected LAN device 'eth0', got '%s'", lan.GetDevice())
	}
	if lan.GetProtocol() != "static" {
		t.Errorf("Expected LAN protocol 'static', got '%s'", lan.GetProtocol())
	}
	if lan.GetIpaddr() != "10.41.254.1" {
		t.Errorf("Expected LAN IP '10.41.254.1', got '%s'", lan.GetIpaddr())
	}
	if lan.GetNetmask() != "255.255.0.0" {
		t.Errorf("Expected LAN netmask '255.255.0.0', got '%s'", lan.GetNetmask())
	}
}

func TestModelGetters(t *testing.T) {
	model := Model{
		ID:   "test-id",
		Name: "Test Name",
	}

	if model.GetID() != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", model.GetID())
	}
	if model.GetName() != "Test Name" {
		t.Errorf("Expected Name 'Test Name', got '%s'", model.GetName())
	}
}

func TestSystemGetters(t *testing.T) {
	system := System{
		Hostname: "test-hostname",
	}

	if system.GetHostname() != "test-hostname" {
		t.Errorf("Expected hostname 'test-hostname', got '%s'", system.GetHostname())
	}
}

func TestNetworkGetters(t *testing.T) {
	network := Network{
		Lan: Lan{
			Device:   "eth0",
			Protocol: "dhcp",
			Ipaddr:   "192.168.1.1",
			Netmask:  "255.255.255.0",
		},
	}

	lan := network.GetLan()
	if lan.GetDevice() != "eth0" {
		t.Errorf("Expected device 'eth0', got '%s'", lan.GetDevice())
	}
	if lan.GetProtocol() != "dhcp" {
		t.Errorf("Expected protocol 'dhcp', got '%s'", lan.GetProtocol())
	}
	if lan.GetIpaddr() != "192.168.1.1" {
		t.Errorf("Expected ipaddr '192.168.1.1', got '%s'", lan.GetIpaddr())
	}
	if lan.GetNetmask() != "255.255.255.0" {
		t.Errorf("Expected netmask '255.255.255.0', got '%s'", lan.GetNetmask())
	}
}

func TestLanGetters(t *testing.T) {
	lan := Lan{
		Device:   "wlan0",
		Protocol: "static",
		Ipaddr:   "10.0.0.1",
		Netmask:  "255.0.0.0",
	}

	if lan.GetDevice() != "wlan0" {
		t.Errorf("Expected device 'wlan0', got '%s'", lan.GetDevice())
	}
	if lan.GetProtocol() != "static" {
		t.Errorf("Expected protocol 'static', got '%s'", lan.GetProtocol())
	}
	if lan.GetIpaddr() != "10.0.0.1" {
		t.Errorf("Expected ipaddr '10.0.0.1', got '%s'", lan.GetIpaddr())
	}
	if lan.GetNetmask() != "255.0.0.0" {
		t.Errorf("Expected netmask '255.0.0.0', got '%s'", lan.GetNetmask())
	}
}

func TestWlanGetters_Phy0(t *testing.T) {
	board := loadTestBoard(t)

	// Test Phy0 getters
	if board.GetPhy0Path() != "platform/soc/fe204000.spi/spi_master/spi0/spi0.0" {
		t.Errorf("Expected Phy0 path, got '%s'", board.GetPhy0Path())
	}
	if board.GetPhy0AntennaRx() != 0 {
		t.Errorf("Expected Phy0 AntennaRx 0, got %d", board.GetPhy0AntennaRx())
	}
	if board.GetPhy0AntennaTx() != 0 {
		t.Errorf("Expected Phy0 AntennaTx 0, got %d", board.GetPhy0AntennaTx())
	}

	bands := board.GetPhy0Bands()
	if !bands.Get5GHt() {
		t.Error("Expected Phy0 5G HT to be true")
	}
	if !bands.Get5GVht() {
		t.Error("Expected Phy0 5G VHT to be true")
	}
	if bands.Get5GMaxWidth() != 160 {
		t.Errorf("Expected Phy0 5G max width 160, got %d", bands.Get5GMaxWidth())
	}
	if bands.Get5GDefaultChannel() != 36 {
		t.Errorf("Expected Phy0 5G default channel 36, got %d", bands.Get5GDefaultChannel())
	}

	modes := bands.Get5GModes()
	expectedModes := []string{"NOHT", "HT20", "VHT20", "HT40", "VHT40", "VHT80", "VHT160"}
	if len(modes) != len(expectedModes) {
		t.Errorf("Expected %d modes, got %d", len(expectedModes), len(modes))
	}
}

func TestWlanGetters_Phy1(t *testing.T) {
	board := loadTestBoard(t)

	// Test Phy1 getters
	if board.GetPhy1Path() != "scb/fd500000.pcie/pci0000:00/0000:00:00.0/0000:01:00.0" {
		t.Errorf("Expected Phy1 path, got '%s'", board.GetPhy1Path())
	}
	if board.GetPhy1AntennaRx() != 3 {
		t.Errorf("Expected Phy1 AntennaRx 3, got %d", board.GetPhy1AntennaRx())
	}
	if board.GetPhy1AntennaTx() != 3 {
		t.Errorf("Expected Phy1 AntennaTx 3, got %d", board.GetPhy1AntennaTx())
	}

	bands := board.GetPhy1Bands()

	// Test 2G band
	if !bands.Get2GHt() {
		t.Error("Expected Phy1 2G HT to be true")
	}
	if !bands.Get2GHe() {
		t.Error("Expected Phy1 2G HE to be true")
	}
	if bands.Get2GMaxWidth() != 40 {
		t.Errorf("Expected Phy1 2G max width 40, got %d", bands.Get2GMaxWidth())
	}
	if bands.Get2GDefaultChannel() != 1 {
		t.Errorf("Expected Phy1 2G default channel 1, got %d", bands.Get2GDefaultChannel())
	}

	// Test 5G band
	if !bands.Get5GHt() {
		t.Error("Expected Phy1 5G HT to be true")
	}
	if !bands.Get5GVht() {
		t.Error("Expected Phy1 5G VHT to be true")
	}
	if !bands.Get5GHe() {
		t.Error("Expected Phy1 5G HE to be true")
	}
	if bands.Get5GMaxWidth() != 160 {
		t.Errorf("Expected Phy1 5G max width 160, got %d", bands.Get5GMaxWidth())
	}

	// Test 6G band
	if !bands.Get6GHe() {
		t.Error("Expected Phy1 6G HE to be true")
	}
	if bands.Get6GMaxWidth() != 160 {
		t.Errorf("Expected Phy1 6G max width 160, got %d", bands.Get6GMaxWidth())
	}
	if bands.Get6GDefaultChannel() != 1 {
		t.Errorf("Expected Phy1 6G default channel 1, got %d", bands.Get6GDefaultChannel())
	}
}

func TestWlanGetters_Phy2(t *testing.T) {
	board := loadTestBoard(t)

	// Test Phy2 getters
	if board.GetPhy2Path() != "platform/soc/fe980000.usb/usb1/1-1/1-1.2/1-1.2:1.0" {
		t.Errorf("Expected Phy2 path, got '%s'", board.GetPhy2Path())
	}
	if board.GetPhy2AntennaRx() != 0 {
		t.Errorf("Expected Phy2 AntennaRx 0, got %d", board.GetPhy2AntennaRx())
	}
	if board.GetPhy2AntennaTx() != 0 {
		t.Errorf("Expected Phy2 AntennaTx 0, got %d", board.GetPhy2AntennaTx())
	}

	bands := board.GetPhy2Bands()
	if !bands.Get2GHt() {
		t.Error("Expected Phy2 2G HT to be true")
	}
	if bands.Get2GMaxWidth() != 40 {
		t.Errorf("Expected Phy2 2G max width 40, got %d", bands.Get2GMaxWidth())
	}
	if bands.Get2GDefaultChannel() != 1 {
		t.Errorf("Expected Phy2 2G default channel 1, got %d", bands.Get2GDefaultChannel())
	}
}
