package mgmt

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digineo/go-uci/v2"
	"github.com/openmanet/openmanetd/internal/iwinfo"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
)

// ── fakeOpenMANETReader ──────────────────────────────────────────────────────

// fakeOpenMANETReader implements network.OpenMANETConfigReader backed by an
// in-memory map. It allows tests to seed data and inject errors.
type fakeOpenMANETReader struct {
	data        map[string]map[string]map[string][]string // config→section→option→values
	sections    map[string]map[string]string              // config→section→type
	commitErr   error
	setTypeErr  error
	commitCalls int
}

func newFakeOpenMANETReader() *fakeOpenMANETReader {
	return &fakeOpenMANETReader{
		data:     make(map[string]map[string]map[string][]string),
		sections: make(map[string]map[string]string),
	}
}

func (f *fakeOpenMANETReader) Get(config, section, option string) ([]string, bool) {
	if f.data[config] == nil {
		return nil, false
	}

	if f.data[config][section] == nil {
		return nil, false
	}

	v, ok := f.data[config][section][option]

	return v, ok
}

func (f *fakeOpenMANETReader) GetSections(config, secType string) ([]string, error) {
	var out []string

	if f.sections[config] != nil {
		for s, t := range f.sections[config] {
			if t == secType {
				out = append(out, s)
			}
		}
	}

	return out, nil
}

func (f *fakeOpenMANETReader) SetType(config, section, option string, _ uci.OptionType, values ...string) error {
	if f.setTypeErr != nil {
		return f.setTypeErr
	}

	if f.data[config] == nil {
		f.data[config] = make(map[string]map[string][]string)
	}

	if f.data[config][section] == nil {
		f.data[config][section] = make(map[string][]string)
	}

	f.data[config][section][option] = values

	return nil
}

func (f *fakeOpenMANETReader) Del(config, section, option string) error {
	if f.data[config] != nil && f.data[config][section] != nil {
		delete(f.data[config][section], option)
	}

	return nil
}

func (f *fakeOpenMANETReader) AddSection(config, section, typ string) error {
	if f.sections[config] == nil {
		f.sections[config] = make(map[string]string)
	}

	f.sections[config][section] = typ
	if f.data[config] == nil {
		f.data[config] = make(map[string]map[string][]string)
	}

	if f.data[config][section] == nil {
		f.data[config][section] = make(map[string][]string)
	}

	return nil
}

func (f *fakeOpenMANETReader) DelSection(config, section string) error {
	if f.data[config] != nil {
		delete(f.data[config], section)
	}

	if f.sections[config] != nil {
		delete(f.sections[config], section)
	}

	return nil
}

func (f *fakeOpenMANETReader) Commit() error {
	f.commitCalls++

	return f.commitErr
}

func (f *fakeOpenMANETReader) ReloadConfig() error {
	return nil
}

// seedBatMesh1Configured seeds batmesh1configured with the given value ("0"/"1").
func (f *fakeOpenMANETReader) seedBatMesh1Configured(value string) {
	_ = f.AddSection("openmanetd", "config", "openmanet")
	_ = f.SetType("openmanetd", "config", "batmesh1configured", uci.TypeOption, value)
}

// ── fakeWirelessReader ───────────────────────────────────────────────────────

// fakeWirelessReader implements network.ConfigReader backed by an in-memory map
// with named sections only (no anonymous-section handling needed for these tests).
type fakeWirelessReader struct {
	data         map[string]map[string]map[string][]string // config→section→option→values
	sectionTypes map[string]map[string]string              // config→section→type
	setTypeErr   error
	commitErr    error
	commitCalls  int
}

func newFakeWirelessReader() *fakeWirelessReader {
	return &fakeWirelessReader{
		data:         make(map[string]map[string]map[string][]string),
		sectionTypes: make(map[string]map[string]string),
	}
}

func (f *fakeWirelessReader) Get(config, section, option string) ([]string, bool) {
	if f.data[config] == nil {
		return nil, false
	}

	if f.data[config][section] == nil {
		return nil, false
	}

	v, ok := f.data[config][section][option]

	return v, ok
}

func (f *fakeWirelessReader) GetSections(config, secType string) ([]string, error) {
	var out []string

	if f.sectionTypes[config] != nil {
		for s, t := range f.sectionTypes[config] {
			if t == secType {
				out = append(out, s)
			}
		}
	}

	return out, nil
}

func (f *fakeWirelessReader) SetType(config, section, option string, _ uci.OptionType, values ...string) error {
	if f.setTypeErr != nil {
		return f.setTypeErr
	}

	if f.data[config] == nil {
		f.data[config] = make(map[string]map[string][]string)
	}

	if f.data[config][section] == nil {
		f.data[config][section] = make(map[string][]string)
	}

	f.data[config][section][option] = values

	return nil
}

func (f *fakeWirelessReader) Del(config, section, option string) error {
	if f.data[config] != nil && f.data[config][section] != nil {
		delete(f.data[config][section], option)
	}

	return nil
}

func (f *fakeWirelessReader) AddSection(config, section, typ string) error {
	if f.sectionTypes[config] == nil {
		f.sectionTypes[config] = make(map[string]string)
	}

	f.sectionTypes[config][section] = typ
	if f.data[config] == nil {
		f.data[config] = make(map[string]map[string][]string)
	}

	if f.data[config][section] == nil {
		f.data[config][section] = make(map[string][]string)
	}

	return nil
}

func (f *fakeWirelessReader) DelSection(config, section string) error {
	if f.data[config] != nil {
		delete(f.data[config], section)
	}

	if f.sectionTypes[config] != nil {
		delete(f.sectionTypes[config], section)
	}

	return nil
}

func (f *fakeWirelessReader) Commit() error {
	f.commitCalls++

	return f.commitErr
}

func (f *fakeWirelessReader) ReloadConfig() error {
	return nil
}

// seedWifiDevice seeds a named wifi-device section.
func (f *fakeWirelessReader) seedWifiDevice(section, band, channel, htmode string) {
	_ = f.AddSection("wireless", section, "wifi-device")
	if band != "" {
		_ = f.SetType("wireless", section, "band", uci.TypeOption, band)
	}

	if channel != "" {
		_ = f.SetType("wireless", section, "channel", uci.TypeOption, channel)
	}

	if htmode != "" {
		_ = f.SetType("wireless", section, "htmode", uci.TypeOption, htmode)
	}
}

// seedMeshIface seeds a named wifi-iface section with mode=mesh.
func (f *fakeWirelessReader) seedMeshIface(section, device, meshID, key string) {
	_ = f.AddSection("wireless", section, "wifi-iface")
	_ = f.SetType("wireless", section, "device", uci.TypeOption, device)
	_ = f.SetType("wireless", section, "mode", uci.TypeOption, "mesh")
	_ = f.SetType("wireless", section, "mesh_id", uci.TypeOption, meshID)
	_ = f.SetType("wireless", section, "key", uci.TypeOption, key)
	_ = f.SetType("wireless", section, "encryption", uci.TypeOption, "sae")
}

// ── fakeIwinfo ───────────────────────────────────────────────────────────────

// fakeIwinfo implements iwinfo.IwinfoProvider for test purposes.
type fakeIwinfo struct {
	infoMap    map[string]*iwinfo.InterfaceInfo
	infoMapErr error
}

func (f *fakeIwinfo) GetDevices(_ context.Context) ([]string, error) {
	var keys []string
	for k := range f.infoMap {
		keys = append(keys, k)
	}

	return keys, nil
}

func (f *fakeIwinfo) GetInfo(_ context.Context, device string) (*iwinfo.InterfaceInfo, error) {
	if f.infoMapErr != nil {
		return nil, f.infoMapErr
	}

	info, ok := f.infoMap[device]
	if !ok {
		return nil, errors.New("device not found")
	}

	return info, nil
}

func (f *fakeIwinfo) GetInfoForAll(_ context.Context) (map[string]*iwinfo.InterfaceInfo, error) {
	if f.infoMapErr != nil {
		return nil, f.infoMapErr
	}

	return f.infoMap, nil
}

// makeIwinfoWithHardware creates a fakeIwinfo with a single device whose
// Hardware.Name is set to the given value.
func makeIwinfoWithHardware(device, hardwareName string) *fakeIwinfo {
	return &fakeIwinfo{
		infoMap: map[string]*iwinfo.InterfaceInfo{
			device: {
				Hardware: iwinfo.HardwareInfo{Name: hardwareName},
			},
		},
	}
}

// ── helper ───────────────────────────────────────────────────────────────────

// newTestManagementConfig returns a minimal ManagementConfig suitable for unit
// tests. The logger is discarded so tests stay silent.
func newTestManagementConfig() *ManagementConfig {
	return &ManagementConfig{
		Log: zerolog.Nop(),
	}
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestSetupBatMesh1Interface_AlreadyConfigured(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("1")

	wireless := newFakeWirelessReader()
	iw := &fakeIwinfo{} // no calls expected

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err != nil {
		t.Fatalf("expected nil for already-configured, got %v", err)
	}

	// Wireless reader must not have been written to.
	if wireless.commitCalls > 0 {
		t.Error("expected no wireless commits when already configured")
	}
}

func TestSetupBatMesh1Interface_OpenMANETCheckError(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	// Seed an invalid value to cause a parse error inside IsBatMesh1ConfiguredWithReader.
	_ = openmanet.AddSection("openmanetd", "config", "openmanet")
	_ = openmanet.SetType("openmanetd", "config", "batmesh1configured", uci.TypeOption, "invalid")

	wireless := newFakeWirelessReader()
	iw := &fakeIwinfo{}

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err == nil {
		t.Fatal("expected error from invalid batmesh1configured value")
	}

	if !strings.Contains(err.Error(), "check batmesh1 configured") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_IwinfoError(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	iw := &fakeIwinfo{infoMapErr: errors.New("ubus not available")}

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err == nil {
		t.Fatal("expected error when iwinfo fails")
	}

	if !strings.Contains(err.Error(), "get iwinfo for all devices") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_NoSupportedHardware(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	iw := makeIwinfoWithHardware("wlan0", "Broadcom BCM4366")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err == nil {
		t.Fatal("expected error when no supported hardware found")
	}

	if !strings.Contains(err.Error(), "no supported hardware") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_HardwareMatch_MT7915(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	// No mesh iface seeded — expect it to fail past the hardware check.
	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	// Should fail at "no mesh iface", not at "no supported hardware".
	if err == nil {
		t.Fatal("expected error after hardware check (no mesh iface)")
	}

	if strings.Contains(err.Error(), "no supported hardware") {
		t.Errorf("hardware check should have passed for MT7915, got: %v", err)
	}
}

func TestSetupBatMesh1Interface_HardwareMatch_MT7916(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7916AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	// Should fail at "no mesh iface", not at "no supported hardware".
	if err == nil {
		t.Fatal("expected error after hardware check (no mesh iface)")
	}

	if strings.Contains(err.Error(), "no supported hardware") {
		t.Errorf("hardware check should have passed for MT7916, got: %v", err)
	}
}

func TestSetupBatMesh1Interface_NoMeshIface(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	// Seed only a non-mesh iface.
	_ = wireless.AddSection("wireless", "default_radio0", "wifi-iface")
	_ = wireless.SetType("wireless", "default_radio0", "mode", uci.TypeOption, "ap")

	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err == nil {
		t.Fatal("expected error when no mesh iface found")
	}

	if !strings.Contains(err.Error(), "no existing wifi-iface with mode=mesh") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_No2gDevice(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	wireless.seedMeshIface("existing_mesh0", "radio4", "halowmesh", "secretkey")
	// Only a non-2g device.
	wireless.seedWifiDevice("radio4", "s1g", "42", "")

	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err == nil {
		t.Fatal("expected error when no 2g device found")
	}

	if !strings.Contains(err.Error(), "no wifi-device with band=2g") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_Success(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	wireless.seedMeshIface("existing_mesh0", "radio4", "halowmesh", "secretkey999")
	wireless.seedWifiDevice("radio1", "2g", "6", "HT20")

	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify new wifi-iface was created as "default_radio1".
	newIface, ierr := network.GetWirelessIfaceByNameWithReader("default_radio1", wireless)
	if ierr != nil {
		t.Fatalf("failed to read back new iface: %v", ierr)
	}

	if newIface.Device != "radio1" {
		t.Errorf("Device: got %q, want %q", newIface.Device, "radio1")
	}

	if newIface.Network != "batmesh1" {
		t.Errorf("Network: got %q, want %q", newIface.Network, "batmesh1")
	}

	if newIface.Mode != "mesh" {
		t.Errorf("Mode: got %q, want %q", newIface.Mode, "mesh")
	}

	if newIface.MeshID != "halowmesh" {
		t.Errorf("MeshID: got %q, want %q", newIface.MeshID, "halowmesh")
	}

	if newIface.Key != "secretkey999" {
		t.Errorf("Key: got %q, want %q", newIface.Key, "secretkey999")
	}

	if newIface.MeshFwding != "0" {
		t.Errorf("MeshFwding: got %q, want %q", newIface.MeshFwding, "0")
	}

	if newIface.Encryption != "sae" {
		t.Errorf("Encryption: got %q, want %q", newIface.Encryption, "sae")
	}

	// Verify radio1 was updated.
	radio, rerr := network.GetWirelessDeviceByNameWithReader("radio1", wireless)
	if rerr != nil {
		t.Fatalf("failed to read back radio1: %v", rerr)
	}

	if radio.Channel != "8" {
		t.Errorf("Channel: got %q, want %q", radio.Channel, "8")
	}

	if radio.HTMode != "HE20" {
		t.Errorf("HTMode: got %q, want %q", radio.HTMode, "HE20")
	}

	// Verify band=2g is preserved (was set during seed).
	if radio.Band != "2g" {
		t.Errorf("Band should be preserved: got %q, want %q", radio.Band, "2g")
	}

	// Verify batmesh1configured was set to 1.
	configured, cerr := network.IsBatMesh1ConfiguredWithReader(openmanet)
	if cerr != nil {
		t.Fatalf("IsBatMesh1ConfiguredWithReader failed: %v", cerr)
	}

	if !configured {
		t.Error("expected batmesh1configured to be true after success")
	}
}

func TestSetupBatMesh1Interface_SetIfaceError(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	wireless.seedMeshIface("existing_mesh0", "radio4", "halowmesh", "secretkey")
	wireless.seedWifiDevice("radio1", "2g", "6", "HT20")
	wireless.setTypeErr = errors.New("UCI write failure")

	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err == nil {
		t.Fatal("expected error when SetWirelessIface fails")
	}

	if !strings.Contains(err.Error(), "create wifi-iface") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_SetDeviceError(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	wireless.seedMeshIface("existing_mesh0", "radio4", "halowmesh", "secretkey")
	wireless.seedWifiDevice("radio1", "2g", "6", "HT20")

	// Inject commit error so the iface creation succeeds (SetType), but the
	// device update commit fails.
	wireless.commitErr = errors.New("commit failure")

	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err == nil {
		t.Fatal("expected error when Commit fails")
	}

	// Error should relate to either iface or device update.
	if !strings.Contains(err.Error(), "create wifi-iface") && !strings.Contains(err.Error(), "update wifi-device") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_SetConfiguredError(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")
	openmanet.commitErr = errors.New("openmanet commit failure")

	wireless := newFakeWirelessReader()
	wireless.seedMeshIface("existing_mesh0", "radio4", "halowmesh", "secretkey")
	wireless.seedWifiDevice("radio1", "2g", "6", "HT20")

	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err == nil {
		t.Fatal("expected error when openmanet Commit fails")
	}

	if !strings.Contains(err.Error(), "mark batmesh1 configured") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_IdempotentWhenAlreadyConfigured(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("1")

	wireless := newFakeWirelessReader()
	// Poison the wireless reader to ensure it is never touched.
	wireless.setTypeErr = errors.New("should not be called")

	iw := &fakeIwinfo{infoMapErr: errors.New("should not be called")}

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw)
	if err != nil {
		t.Fatalf("expected nil for already-configured (idempotency check), got %v", err)
	}
}
