package mgmt

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digineo/go-uci/v2"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/iwinfo"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, nil)
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, nil)
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, nil)
	if err == nil {
		t.Fatal("expected error when iwinfo fails")
	}

	if !strings.Contains(err.Error(), "get iwinfo for all devices") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSetupBatMesh1Interface_HardwareMatch_MT7915(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	wireless.seedWifiDevice("radio1", "2g", "6", "HT20")
	// No mesh iface seeded — expect it to fail past the hardware check.
	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, wirelessStatusForRadio(t, "radio1", "wlan0"))
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
	wireless.seedWifiDevice("radio1", "2g", "6", "HT20")

	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7916AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, wirelessStatusForRadio(t, "radio1", "wlan0"))
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
	wireless.seedWifiDevice("radio1", "2g", "6", "HT20")

	iw := makeIwinfoWithHardware("wlan0", "MediaTek MT7915AN")

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, wirelessStatusForRadio(t, "radio1", "wlan0"))
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, wirelessStatusForRadio(t, "radio4", "wlan0"))
	if err != nil {
		t.Fatalf("expected safe no-op when no supported 2.4 GHz radio exists, got %v", err)
	}

	if wireless.commitCalls != 0 || openmanet.commitCalls != 0 {
		t.Fatal("unsupported radio configuration was modified")
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, wirelessStatusForRadio(t, "radio1", "wlan0"))
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

func TestSetupBatMesh1Interface_doesNotTouchUnrelated2gRadio(t *testing.T) {
	m := newTestManagementConfig()
	openmanet := newFakeOpenMANETReader()
	openmanet.seedBatMesh1Configured("0")

	wireless := newFakeWirelessReader()
	wireless.seedMeshIface("existing_mesh0", "radio4", "halowmesh", "secretkey")
	wireless.seedWifiDevice("radio0", "2g", "1", "HT20")
	wireless.seedWifiDevice("radio1", "2g", "6", "HT20")

	iw := &fakeIwinfo{infoMap: map[string]*iwinfo.InterfaceInfo{
		"phy0-ap0":   {Hardware: iwinfo.HardwareInfo{Name: "Broadcom BCM43430"}},
		"phy1-mesh0": {Hardware: iwinfo.HardwareInfo{Name: "MediaTek MT7915AN"}},
	}}
	status := &fakeWirelessStatusProvider{Status: map[string]*network.WirelessRadioStatus{
		"radio0": {Interfaces: []network.WirelessRadioInterface{{Ifname: "phy0-ap0"}}},
		"radio1": {Interfaces: []network.WirelessRadioInterface{{Ifname: "phy1-mesh0"}}},
	}}

	require.NoError(t, m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, status))

	_, createdOnboardMesh := wireless.Get("wireless", "default_radio0", "device")
	assert.False(t, createdOnboardMesh, "must not create batmesh1 on the unrelated onboard radio")

	radio0, err := network.GetWirelessDeviceByNameWithReader("radio0", wireless)
	require.NoError(t, err)
	assert.Equal(t, "1", radio0.Channel)
	assert.Equal(t, "HT20", radio0.HTMode)
	assert.Empty(t, radio0.Disabled)

	iface, err := network.GetWirelessIfaceByNameWithReader("default_radio1", wireless)
	require.NoError(t, err)
	assert.Equal(t, "radio1", iface.Device)
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, wirelessStatusForRadio(t, "radio1", "wlan0"))
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, wirelessStatusForRadio(t, "radio1", "wlan0"))
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, wirelessStatusForRadio(t, "radio1", "wlan0"))
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

	err := m.setupBatMesh1InterfaceWithDeps(context.Background(), openmanet, wireless, iw, nil)
	if err != nil {
		t.Fatalf("expected nil for already-configured (idempotency check), got %v", err)
	}
}

// seedNetworkDevice seeds a named network device section in fakeNetworkReader.
func (f *fakeNetworkReader) seedNetworkDevice(section, name, devType string) {
	_ = f.AddSection("network", section, "device")
	_ = f.SetType("network", section, "name", uci.TypeOption, name)

	if devType != "" {
		_ = f.SetType("network", section, "type", uci.TypeOption, devType)
	}
}

// ── configureDeviceMulticast tests ───────────────────────────────────────────

// fakeMeshConfig returns a getMeshConfig stub that reports gateway mode based on gwMode.
func fakeMeshConfig(gwMode string) func(string) (*batmanadv.MeshConfig, error) {
	return func(_ string) (*batmanadv.MeshConfig, error) {
		return &batmanadv.MeshConfig{GwMode: gwMode}, nil
	}
}

// fakeMeshConfigErr returns a getMeshConfig stub that always returns an error.
func fakeMeshConfigErr(err error) func(string) (*batmanadv.MeshConfig, error) {
	return func(_ string) (*batmanadv.MeshConfig, error) {
		return nil, err
	}
}

func TestConfigureDeviceMulticast_GatewayMode(t *testing.T) {
	m := newTestManagementConfig()
	m.IFace = "br-ahwlan"
	m.BatInterface = "bat0"

	reader := newFakeNetworkReader()
	reader.seedNetworkDevice("dev0", "br-ahwlan", "bridge")

	var reloadCalled bool

	reloadFn := func(_ context.Context) error {
		reloadCalled = true

		return nil
	}

	err := m.configureDeviceMulticastWithDeps(context.Background(), reader, fakeMeshConfig("server"), reloadFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	device, derr := network.GetDeviceByNameWithReader("br-ahwlan", reader)
	if derr != nil {
		t.Fatalf("failed to read back device: %v", derr)
	}

	if device.IgmpSnooping != "1" {
		t.Errorf("IgmpSnooping: got %q, want %q", device.IgmpSnooping, "1")
	}

	if device.MulticastQuerier != "1" {
		t.Errorf("MulticastQuerier: got %q, want %q", device.MulticastQuerier, "1")
	}

	if !reloadCalled {
		t.Error("expected reload to be called")
	}
}

func TestConfigureDeviceMulticast_NonGatewayMode(t *testing.T) {
	m := newTestManagementConfig()
	m.IFace = "br-ahwlan"
	m.BatInterface = "bat0"

	reader := newFakeNetworkReader()
	reader.seedNetworkDevice("dev0", "br-ahwlan", "bridge")

	var reloadCalled bool

	reloadFn := func(_ context.Context) error {
		reloadCalled = true

		return nil
	}

	err := m.configureDeviceMulticastWithDeps(context.Background(), reader, fakeMeshConfig("client"), reloadFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	device, derr := network.GetDeviceByNameWithReader("br-ahwlan", reader)
	if derr != nil {
		t.Fatalf("failed to read back device: %v", derr)
	}

	if device.IgmpSnooping != "1" {
		t.Errorf("IgmpSnooping: got %q, want %q", device.IgmpSnooping, "1")
	}

	if device.MulticastQuerier != "0" {
		t.Errorf("MulticastQuerier: got %q, want %q", device.MulticastQuerier, "0")
	}

	if !reloadCalled {
		t.Error("expected reload to be called")
	}
}

func TestConfigureDeviceMulticast_DeviceNotFound(t *testing.T) {
	m := newTestManagementConfig()
	m.IFace = "br-missing"
	m.BatInterface = "bat0"

	reader := newFakeNetworkReader()
	reloadFn := func(_ context.Context) error { return nil }

	err := m.configureDeviceMulticastWithDeps(context.Background(), reader, fakeMeshConfig("server"), reloadFn)
	if err == nil {
		t.Fatal("expected error when device not found")
	}

	if !strings.Contains(err.Error(), "get device br-missing") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfigureDeviceMulticast_MeshConfigError(t *testing.T) {
	m := newTestManagementConfig()
	m.IFace = "br-ahwlan"
	m.BatInterface = "bat0"

	reader := newFakeNetworkReader()
	reader.seedNetworkDevice("dev0", "br-ahwlan", "bridge")

	reloadFn := func(_ context.Context) error { return nil }

	err := m.configureDeviceMulticastWithDeps(
		context.Background(),
		reader,
		fakeMeshConfigErr(errors.New("batctl not available")),
		reloadFn,
	)
	if err == nil {
		t.Fatal("expected error when getMeshConfig fails")
	}

	if !strings.Contains(err.Error(), "get mesh config") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfigureDeviceMulticast_SetDeviceConfigError(t *testing.T) {
	m := newTestManagementConfig()
	m.IFace = "br-ahwlan"
	m.BatInterface = "bat0"

	reader := newFakeNetworkReader()
	reader.seedNetworkDevice("dev0", "br-ahwlan", "bridge")
	reader.commitErr = errors.New("commit failure")

	reloadFn := func(_ context.Context) error { return nil }

	err := m.configureDeviceMulticastWithDeps(context.Background(), reader, fakeMeshConfig("server"), reloadFn)
	if err == nil {
		t.Fatal("expected error when SetDeviceConfig fails")
	}

	if !strings.Contains(err.Error(), "set device config br-ahwlan") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfigureDeviceMulticast_ReloadError(t *testing.T) {
	m := newTestManagementConfig()
	m.IFace = "br-ahwlan"
	m.BatInterface = "bat0"

	reader := newFakeNetworkReader()
	reader.seedNetworkDevice("dev0", "br-ahwlan", "bridge")

	reloadFn := func(_ context.Context) error {
		return errors.New("reload failed")
	}

	err := m.configureDeviceMulticastWithDeps(context.Background(), reader, fakeMeshConfig("server"), reloadFn)
	if err == nil {
		t.Fatal("expected error when reload fails")
	}

	if !strings.Contains(err.Error(), "reload config") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ── configureBatmanForceflood tests ─────────────────────────────────────────

func TestConfigureBatmanForceflood_Enabled(t *testing.T) {
	m := newTestManagementConfig()
	m.BatInterface = "bat0"
	m.BatmanMulticastForceflood = true

	reader := newFakeNetworkReader()

	var reloadCalled bool

	reloadFn := func(_ context.Context) error {
		reloadCalled = true

		return nil
	}

	err := m.configureBatmanForcefloodWithDeps(context.Background(), reader, reloadFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	values, ok := reader.Get("network", "bat0", "multicast_mode")
	if !ok {
		t.Fatal("expected multicast_mode to be written to bat0 section")
	}

	if len(values) != 1 || values[0] != "1" {
		t.Errorf("multicast_mode: got %v, want [1]", values)
	}

	if !reloadCalled {
		t.Error("expected reload to be called")
	}
}

func TestConfigureBatmanForceflood_Disabled(t *testing.T) {
	m := newTestManagementConfig()
	m.BatInterface = "bat0"
	m.BatmanMulticastForceflood = false

	reader := newFakeNetworkReader()
	reloadFn := func(_ context.Context) error { return nil }

	err := m.configureBatmanForcefloodWithDeps(context.Background(), reader, reloadFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	values, ok := reader.Get("network", "bat0", "multicast_mode")
	if !ok {
		t.Fatal("expected multicast_mode to be written to bat0 section")
	}

	if len(values) != 1 || values[0] != "0" {
		t.Errorf("multicast_mode: got %v, want [0]", values)
	}
}

func TestConfigureBatmanForceflood_CommitError(t *testing.T) {
	m := newTestManagementConfig()
	m.BatInterface = "bat0"
	m.BatmanMulticastForceflood = true

	reader := newFakeNetworkReader()
	reader.commitErr = errors.New("commit failure")

	reloadFn := func(_ context.Context) error { return nil }

	err := m.configureBatmanForcefloodWithDeps(context.Background(), reader, reloadFn)
	if err == nil {
		t.Fatal("expected error when commit fails")
	}

	if !strings.Contains(err.Error(), "set multicast_mode on bat0") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfigureBatmanForceflood_ReloadError(t *testing.T) {
	m := newTestManagementConfig()
	m.BatInterface = "bat0"
	m.BatmanMulticastForceflood = true

	reader := newFakeNetworkReader()

	reloadFn := func(_ context.Context) error {
		return errors.New("reload failed")
	}

	err := m.configureBatmanForcefloodWithDeps(context.Background(), reader, reloadFn)
	if err == nil {
		t.Fatal("expected error when reload fails")
	}

	if !strings.Contains(err.Error(), "reload config") {
		t.Errorf("unexpected error message: %v", err)
	}
}
