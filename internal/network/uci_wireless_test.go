package network

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGetWirelessMeshPassphraseFromPath(t *testing.T) {
	path := filepath.Join("..", "..", "testfixtures", "uci", "wireless")

	key, err := GetWirelessMeshPassphraseFromPath(path)
	if err != nil {
		t.Fatalf("GetWirelessMeshPassphraseFromPath failed: %v", err)
	}

	if key != "thisisnotarealpassword" {
		t.Fatalf("expected mesh passphrase %q, got %q", "thisisnotarealpassword", key)
	}
}

func TestGetWirelessMeshPassphraseFromPathSkipsDisabled(t *testing.T) {
	path := writeWirelessFixture(t, `
config wifi-iface 'mesh_disabled'
	option mode 'mesh'
	option encryption 'sae'
	option key 'disabled-key'
	option disabled '1'

config wifi-iface 'mesh_enabled'
	option mode 'mesh'
	option encryption 'sae'
	option key 'enabled-key'
`)

	key, err := GetWirelessMeshPassphraseFromPath(path)
	if err != nil {
		t.Fatalf("GetWirelessMeshPassphraseFromPath failed: %v", err)
	}

	if key != "enabled-key" {
		t.Fatalf("expected mesh passphrase %q, got %q", "enabled-key", key)
	}
}

func TestGetWirelessMeshPassphraseFromPathMissingKey(t *testing.T) {
	path := writeWirelessFixture(t, `
config wifi-iface 'mesh0'
	option mode 'mesh'
	option encryption 'sae'
`)

	_, err := GetWirelessMeshPassphraseFromPath(path)
	if err == nil {
		t.Fatalf("expected error when mesh key is missing")
	}

	if !strings.Contains(err.Error(), "missing key") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestGetWirelessMeshPassphraseFromPathNoMeshInterface(t *testing.T) {
	path := writeWirelessFixture(t, `
config wifi-iface 'ap0'
	option mode 'ap'
	option key 'ap-password'
`)

	_, err := GetWirelessMeshPassphraseFromPath(path)
	if err == nil {
		t.Fatalf("expected error when no mesh interface exists")
	}

	if !strings.Contains(err.Error(), "no enabled mesh") {
		t.Fatalf("expected no mesh section error, got %v", err)
	}
}

func writeWirelessFixture(t *testing.T, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "wireless")

	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write wireless fixture: %v", err)
	}

	return path
}

// newWirelessMockReader returns a mockConfigReader pre-populated with wireless config
// data matching the sample OpenWrt wireless configuration.
func newWirelessMockReader() *mockConfigReader {
	return &mockConfigReader{
		data: map[string]map[string]map[string][]string{
			"wireless": {
				"radio1": {
					"type":         {"mac80211"},
					"path":         {"scb/fd500000.pcie/pci0000:00/0000:00:00.0/0000:01:00.0"},
					"band":         {"2g"},
					"channel":      {"8"},
					"htmode":       {"HE20"},
					"country":      {"US"},
					"cell_density": {"0"},
				},
				"radio4": {
					"type":                      {"morse"},
					"path":                      {"platform/soc/fe204000.spi/spi_master/spi0/spi0.0"},
					"band":                      {"s1g"},
					"hwmode":                    {"11ah"},
					"reconf":                    {"0"},
					"enable_mcast_whitelist":    {"0"},
					"enable_mcast_rate_control": {"1"},
					"country":                   {"US"},
					"enable_ps":                 {"0"},
					"enable_dynamic_ps_offload": {"0"},
					"enable_twt":                {"0"},
					"bcf":                       {"bcf_fgh100mhaamd.bin"},
					"channel":                   {"42"},
				},
				"default_radio1": {
					"device":               {"radio1"},
					"network":              {"batmesh1"},
					"mode":                 {"mesh"},
					"key":                  {"rmx4110vqqpfj"},
					"mesh_id":              {"halowmesh"},
					"mesh_fwding":          {"0"},
					"mesh_rssi_threshold":  {"0"},
					"encryption":           {"sae"},
				},
				"default_radio4": {
					"mode":       {"mesh"},
					"device":     {"radio4"},
					"network":    {"batmesh0"},
					"ssid":       {"BCM2711-1003"},
					"encryption": {"sae"},
					"key":        {"rmx4110vqqpfj"},
					"beacon_int": {"1000"},
					"mesh_id":    {"halowmesh"},
				},
			},
		},
		sectionTypes: map[string]map[string]string{
			"wireless": {
				"radio1":         "wifi-device",
				"radio4":         "wifi-device",
				"default_radio1": "wifi-iface",
				"default_radio4": "wifi-iface",
			},
		},
	}
}

// --- GetWirelessDeviceByNameWithReader ---

func TestGetWirelessDeviceByNameWithReader_Radio1(t *testing.T) {
	reader := newWirelessMockReader()

	want := &UCIWirelessDevice{
		Type:        "mac80211",
		Path:        "scb/fd500000.pcie/pci0000:00/0000:00:00.0/0000:01:00.0",
		Band:        "2g",
		Channel:     "8",
		HTMode:      "HE20",
		Country:     "US",
		CellDensity: "0",
	}

	got, err := GetWirelessDeviceByNameWithReader("radio1", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetWirelessDeviceByNameWithReader_Radio4(t *testing.T) {
	reader := newWirelessMockReader()

	want := &UCIWirelessDevice{
		Type:                   "morse",
		Path:                   "platform/soc/fe204000.spi/spi_master/spi0/spi0.0",
		Band:                   "s1g",
		Channel:                "42",
		Country:                "US",
		HWMode:                 "11ah",
		Reconf:                 "0",
		EnableMcastWhitelist:   "0",
		EnableMcastRateControl: "1",
		EnablePS:               "0",
		EnableDynamicPSOffload: "0",
		EnableTWT:              "0",
		BCF:                    "bcf_fgh100mhaamd.bin",
	}

	got, err := GetWirelessDeviceByNameWithReader("radio4", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetWirelessDeviceByNameWithReader_NonExistent(t *testing.T) {
	reader := newWirelessMockReader()

	got, err := GetWirelessDeviceByNameWithReader("radio99", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, &UCIWirelessDevice{}) {
		t.Errorf("expected empty device config for nonexistent section, got %+v", got)
	}
}

// --- GetWirelessIfaceByNameWithReader ---

func TestGetWirelessIfaceByNameWithReader_DefaultRadio1(t *testing.T) {
	reader := newWirelessMockReader()

	want := &UCIWirelessIface{
		Device:            "radio1",
		Network:           "batmesh1",
		Mode:              "mesh",
		Key:               "rmx4110vqqpfj",
		MeshID:            "halowmesh",
		MeshFwding:        "0",
		MeshRSSIThreshold: "0",
		Encryption:        "sae",
	}

	got, err := GetWirelessIfaceByNameWithReader("default_radio1", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetWirelessIfaceByNameWithReader_DefaultRadio4(t *testing.T) {
	reader := newWirelessMockReader()

	want := &UCIWirelessIface{
		Device:     "radio4",
		Network:    "batmesh0",
		Mode:       "mesh",
		Key:        "rmx4110vqqpfj",
		MeshID:     "halowmesh",
		Encryption: "sae",
		SSID:       "BCM2711-1003",
		BeaconInt:  "1000",
	}

	got, err := GetWirelessIfaceByNameWithReader("default_radio4", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetWirelessIfaceByNameWithReader_NonExistent(t *testing.T) {
	reader := newWirelessMockReader()

	got, err := GetWirelessIfaceByNameWithReader("nonexistent", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, &UCIWirelessIface{}) {
		t.Errorf("expected empty iface config for nonexistent section, got %+v", got)
	}
}

// --- SetWirelessDeviceConfigWithReader ---

func TestSetWirelessDeviceConfigWithReader_Complete(t *testing.T) {
	reader := newWirelessMockReader()

	cfg := &UCIWirelessDevice{
		Type:    "mac80211",
		Band:    "5g",
		Channel: "36",
		HTMode:  "HE80",
		Country: "US",
	}

	if err := SetWirelessDeviceConfigWithReader("radio2", cfg, reader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	got, err := GetWirelessDeviceByNameWithReader("radio2", reader)
	if err != nil {
		t.Fatalf("unexpected error reading back config: %v", err)
	}

	if got.Band != "5g" {
		t.Errorf("Band: got %q, want %q", got.Band, "5g")
	}

	if got.Channel != "36" {
		t.Errorf("Channel: got %q, want %q", got.Channel, "36")
	}

	if got.HTMode != "HE80" {
		t.Errorf("HTMode: got %q, want %q", got.HTMode, "HE80")
	}
}

func TestSetWirelessDeviceConfigWithReader_MinimalConfig(t *testing.T) {
	reader := newWirelessMockReader()

	cfg := &UCIWirelessDevice{
		Band:    "2g",
		Channel: "1",
	}

	if err := SetWirelessDeviceConfigWithReader("radio3", cfg, reader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := GetWirelessDeviceByNameWithReader("radio3", reader)
	if err != nil {
		t.Fatalf("unexpected error reading back config: %v", err)
	}

	if got.Band != "2g" {
		t.Errorf("Band: got %q, want %q", got.Band, "2g")
	}

	if got.Type != "" {
		t.Errorf("Type should be empty for minimal config, got %q", got.Type)
	}
}

func TestSetWirelessDeviceConfigWithReader_NilConfig(t *testing.T) {
	reader := newWirelessMockReader()

	err := SetWirelessDeviceConfigWithReader("radio0", nil, reader)
	if err == nil {
		t.Fatal("expected error for nil config")
	}

	if !strings.Contains(err.Error(), "cannot be nil") {
		t.Errorf("expected nil config error, got %v", err)
	}
}

func TestSetWirelessDeviceConfigWithReader_SetTypeError(t *testing.T) {
	reader := newWirelessMockReader()
	reader.setTypeError = errors.New("set type failed")

	cfg := &UCIWirelessDevice{Type: "mac80211"}

	err := SetWirelessDeviceConfigWithReader("radio0", cfg, reader)
	if err == nil {
		t.Fatal("expected error when SetType fails")
	}

	if !strings.Contains(err.Error(), "failed to set type") {
		t.Errorf("expected set type error, got %v", err)
	}
}

func TestSetWirelessDeviceConfigWithReader_CommitError(t *testing.T) {
	reader := newWirelessMockReader()
	reader.commitError = errors.New("commit failed")

	cfg := &UCIWirelessDevice{Band: "2g"}

	err := SetWirelessDeviceConfigWithReader("radio0", cfg, reader)
	if err == nil {
		t.Fatal("expected error when Commit fails")
	}

	if !strings.Contains(err.Error(), "failed to commit") {
		t.Errorf("expected commit error, got %v", err)
	}
}

// --- SetWirelessIfaceConfigWithReader ---

func TestSetWirelessIfaceConfigWithReader_MeshIface(t *testing.T) {
	reader := newWirelessMockReader()

	cfg := &UCIWirelessIface{
		Device:     "radio1",
		Network:    "batmesh1",
		Mode:       "mesh",
		Key:        "newsecretkey",
		MeshID:     "newmesh",
		Encryption: "sae",
	}

	if err := SetWirelessIfaceConfigWithReader("mesh_new", cfg, reader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	got, err := GetWirelessIfaceByNameWithReader("mesh_new", reader)
	if err != nil {
		t.Fatalf("unexpected error reading back config: %v", err)
	}

	if got.Mode != "mesh" {
		t.Errorf("Mode: got %q, want %q", got.Mode, "mesh")
	}

	if got.Key != "newsecretkey" {
		t.Errorf("Key: got %q, want %q", got.Key, "newsecretkey")
	}

	if got.MeshID != "newmesh" {
		t.Errorf("MeshID: got %q, want %q", got.MeshID, "newmesh")
	}
}

func TestSetWirelessIfaceConfigWithReader_WithBeaconAndSSID(t *testing.T) {
	reader := newWirelessMockReader()

	cfg := &UCIWirelessIface{
		Device:     "radio4",
		Network:    "batmesh0",
		Mode:       "mesh",
		SSID:       "MyMesh",
		BeaconInt:  "500",
		Encryption: "sae",
		Key:        "meshkey123",
	}

	if err := SetWirelessIfaceConfigWithReader("default_radio4", cfg, reader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := GetWirelessIfaceByNameWithReader("default_radio4", reader)
	if err != nil {
		t.Fatalf("unexpected error reading back config: %v", err)
	}

	if got.SSID != "MyMesh" {
		t.Errorf("SSID: got %q, want %q", got.SSID, "MyMesh")
	}

	if got.BeaconInt != "500" {
		t.Errorf("BeaconInt: got %q, want %q", got.BeaconInt, "500")
	}
}

func TestSetWirelessIfaceConfigWithReader_NilConfig(t *testing.T) {
	reader := newWirelessMockReader()

	err := SetWirelessIfaceConfigWithReader("mesh0", nil, reader)
	if err == nil {
		t.Fatal("expected error for nil config")
	}

	if !strings.Contains(err.Error(), "cannot be nil") {
		t.Errorf("expected nil config error, got %v", err)
	}
}

func TestSetWirelessIfaceConfigWithReader_SetTypeError(t *testing.T) {
	reader := newWirelessMockReader()
	reader.setTypeError = errors.New("set type failed")

	cfg := &UCIWirelessIface{Device: "radio1"}

	err := SetWirelessIfaceConfigWithReader("mesh0", cfg, reader)
	if err == nil {
		t.Fatal("expected error when SetType fails")
	}

	if !strings.Contains(err.Error(), "failed to set device") {
		t.Errorf("expected set device error, got %v", err)
	}
}

func TestSetWirelessIfaceConfigWithReader_CommitError(t *testing.T) {
	reader := newWirelessMockReader()
	reader.commitError = errors.New("commit failed")

	cfg := &UCIWirelessIface{Mode: "mesh"}

	err := SetWirelessIfaceConfigWithReader("mesh0", cfg, reader)
	if err == nil {
		t.Fatal("expected error when Commit fails")
	}

	if !strings.Contains(err.Error(), "failed to commit") {
		t.Errorf("expected commit error, got %v", err)
	}
}

// --- DeleteWirelessDeviceWithReader ---

func TestDeleteWirelessDeviceWithReader(t *testing.T) {
	reader := newWirelessMockReader()

	if err := DeleteWirelessDeviceWithReader("radio1", reader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	// Verify the section is gone.
	got, err := GetWirelessDeviceByNameWithReader("radio1", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, &UCIWirelessDevice{}) {
		t.Errorf("expected empty device after deletion, got %+v", got)
	}
}

func TestDeleteWirelessDeviceWithReader_DelSectionError(t *testing.T) {
	reader := newWirelessMockReader()
	reader.delSectionErr = errors.New("del section failed")

	err := DeleteWirelessDeviceWithReader("radio1", reader)
	if err == nil {
		t.Fatal("expected error when DelSection fails")
	}

	if !strings.Contains(err.Error(), "failed to delete wireless device section") {
		t.Errorf("expected delete section error, got %v", err)
	}
}

func TestDeleteWirelessDeviceWithReader_CommitError(t *testing.T) {
	reader := newWirelessMockReader()
	reader.commitError = errors.New("commit failed")

	err := DeleteWirelessDeviceWithReader("radio1", reader)
	if err == nil {
		t.Fatal("expected error when Commit fails")
	}

	if !strings.Contains(err.Error(), "failed to commit") {
		t.Errorf("expected commit error, got %v", err)
	}
}

// --- DeleteWirelessIfaceWithReader ---

func TestDeleteWirelessIfaceWithReader(t *testing.T) {
	reader := newWirelessMockReader()

	if err := DeleteWirelessIfaceWithReader("default_radio1", reader); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reader.commitCalled {
		t.Error("expected Commit to be called")
	}

	// Verify the section is gone.
	got, err := GetWirelessIfaceByNameWithReader("default_radio1", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(got, &UCIWirelessIface{}) {
		t.Errorf("expected empty iface after deletion, got %+v", got)
	}
}

func TestDeleteWirelessIfaceWithReader_DelSectionError(t *testing.T) {
	reader := newWirelessMockReader()
	reader.delSectionErr = errors.New("del section failed")

	err := DeleteWirelessIfaceWithReader("default_radio1", reader)
	if err == nil {
		t.Fatal("expected error when DelSection fails")
	}

	if !strings.Contains(err.Error(), "failed to delete wireless iface section") {
		t.Errorf("expected delete section error, got %v", err)
	}
}

func TestDeleteWirelessIfaceWithReader_CommitError(t *testing.T) {
	reader := newWirelessMockReader()
	reader.commitError = errors.New("commit failed")

	err := DeleteWirelessIfaceWithReader("default_radio1", reader)
	if err == nil {
		t.Fatal("expected error when Commit fails")
	}

	if !strings.Contains(err.Error(), "failed to commit") {
		t.Errorf("expected commit error, got %v", err)
	}
}
