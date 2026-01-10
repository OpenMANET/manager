package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewBoard(t *testing.T) {
	// Use the test fixture from testfixtures/uci/board.json
	boardPath := filepath.Join("..", "..", "..", "testfixtures", "uci", "board.json")

	data, err := os.ReadFile(boardPath)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	var board Board
	if err := json.Unmarshal(data, &board); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify the data was unmarshaled correctly
	if board.Model.ID != "bcm2711,mm6108-spi" {
		t.Errorf("Expected model ID 'bcm2711,mm6108-spi', got '%s'", board.Model.ID)
	}
	if board.Model.Name != "RPI RPI4-MM6108 (SPI)" {
		t.Errorf("Expected model name 'RPI RPI4-MM6108 (SPI)', got '%s'", board.Model.Name)
	}
	if board.System.Hostname != "BCM2711-88ba" {
		t.Errorf("Expected hostname 'BCM2711-88ba', got '%s'", board.System.Hostname)
	}
	if board.Network.Lan.Device != "eth0" {
		t.Errorf("Expected LAN device 'eth0', got '%s'", board.Network.Lan.Device)
	}
	if board.Network.Lan.Ipaddr != "10.41.254.1" {
		t.Errorf("Expected LAN IP '10.41.254.1', got '%s'", board.Network.Lan.Ipaddr)
	}
	if board.Wlan.Phy0.Path != "platform/soc/fe204000.spi/spi_master/spi0/spi0.0" {
		t.Errorf("Expected phy0 path, got '%s'", board.Wlan.Phy0.Path)
	}
	if !board.Wlan.Phy0.Info.Bands.FiveG.Ht {
		t.Error("Expected phy0 5G HT to be true")
	}
	if !board.Wlan.Phy0.Info.Bands.FiveG.Vht {
		t.Error("Expected phy0 5G VHT to be true")
	}
	if board.Wlan.Phy0.Info.Bands.FiveG.MaxWidth != 160 {
		t.Errorf("Expected phy0 5G max width 160, got %d", board.Wlan.Phy0.Info.Bands.FiveG.MaxWidth)
	}
}

func TestNewBoard_FileNotFound(t *testing.T) {
	// NewBoard looks for /etc/board.json which likely doesn't exist in test environment
	_, err := NewBoardConfigInfo()
	if err == nil {
		t.Error("Expected error when /etc/board.json doesn't exist, got nil")
	}
}

func TestNewBoard_InvalidJSON(t *testing.T) {
	// Test with invalid JSON
	invalidJSON := []byte(`{"model": invalid}`)
	var board Board
	err := json.Unmarshal(invalidJSON, &board)
	if err == nil {
		t.Error("Expected error with invalid JSON, got nil")
	}
}
