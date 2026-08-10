package iwinfo_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/openmanet/openmanetd/internal/iwinfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── test doubles ──────────────────────────────────────────────────────────────

// mockUbusExecutor always returns the same output and error regardless of args.
type mockUbusExecutor struct {
	output []byte
	err    error
}

func (m *mockUbusExecutor) Execute(_ context.Context, _ ...string) ([]byte, error) {
	return m.output, m.err
}

// mockResponse bundles an output/error pair for dispatchUbusExecutor.
type mockResponse struct {
	output []byte
	err    error
}

// dispatchUbusExecutor returns different responses keyed by the last argument
// passed to Execute (the device JSON selector or the method name).
// It also records every call for argument-capture assertions.
type dispatchUbusExecutor struct {
	calls     [][]string
	responses map[string]mockResponse
	fallback  mockResponse
}

func (d *dispatchUbusExecutor) Execute(_ context.Context, args ...string) ([]byte, error) {
	copied := make([]string, len(args))
	copy(copied, args)
	d.calls = append(d.calls, copied)

	key := ""
	if len(args) > 0 {
		key = args[len(args)-1]
	}

	if resp, ok := d.responses[key]; ok {
		return resp.output, resp.err
	}

	return d.fallback.output, d.fallback.err
}

// ── fixture helpers ───────────────────────────────────────────────────────────

func readFixture(t *testing.T, filename string) []byte {
	t.Helper()

	data, err := os.ReadFile("../../testfixtures/iwinfo/" + filename)
	require.NoError(t, err, "read fixture %s", filename)

	return data
}

// ── GetDevicesWithExecutor ────────────────────────────────────────────────────

func TestGetDevicesWithExecutor_Success(t *testing.T) {
	mock := &mockUbusExecutor{output: readFixture(t, "devices.json")}

	devices, err := iwinfo.GetDevicesWithExecutor(context.Background(), mock)

	require.NoError(t, err)
	assert.Equal(t, []string{"phy1-mesh0", "wlh-10-04", "wlh0"}, devices)
}

func TestGetDevicesWithExecutor_EmptyList(t *testing.T) {
	mock := &mockUbusExecutor{output: []byte(`{"devices":[]}`)}

	devices, err := iwinfo.GetDevicesWithExecutor(context.Background(), mock)

	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestGetDevicesWithExecutor_MissingKey(t *testing.T) {
	// JSON object present but no "devices" key — returns nil slice, no error.
	mock := &mockUbusExecutor{output: []byte(`{}`)}

	devices, err := iwinfo.GetDevicesWithExecutor(context.Background(), mock)

	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestGetDevicesWithExecutor_UbusError(t *testing.T) {
	mock := &mockUbusExecutor{err: errors.New("ubus: connection refused")}

	_, err := iwinfo.GetDevicesWithExecutor(context.Background(), mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ubus: connection refused")
}

func TestGetDevicesWithExecutor_MalformedJSON(t *testing.T) {
	mock := &mockUbusExecutor{output: []byte(`not json at all`)}

	_, err := iwinfo.GetDevicesWithExecutor(context.Background(), mock)

	require.Error(t, err)
}

func TestGetDevicesWithExecutor_EmptyOutput(t *testing.T) {
	mock := &mockUbusExecutor{output: []byte(``)}

	_, err := iwinfo.GetDevicesWithExecutor(context.Background(), mock)

	require.Error(t, err)
}

// ── GetInfoWithExecutor ───────────────────────────────────────────────────────

func TestGetInfoWithExecutor_MediaTek(t *testing.T) {
	mock := &mockUbusExecutor{output: readFixture(t, "info_mediatek.json")}

	info, err := iwinfo.GetInfoWithExecutor(context.Background(), mock, "phy1-mesh0")

	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, "phy1", info.PHY)
	assert.Equal(t, "halowmesh", info.SSID)
	assert.Equal(t, "00:0A:52:0B:7D:AE", info.BSSID)
	assert.Equal(t, "US", info.Country)
	assert.Equal(t, "Mesh Point", info.Mode)
	assert.Equal(t, 8, info.Channel)
	assert.Equal(t, 8, info.CenterChan1)
	assert.Equal(t, 2447, info.Frequency)
	assert.Equal(t, 17, info.TxPower)
	assert.Equal(t, 60, info.Quality)
	assert.Equal(t, 70, info.QualityMax)
	assert.Equal(t, -50, info.Signal)
	assert.Equal(t, -77, info.Noise)
	assert.Equal(t, 51300, info.Bitrate)
	assert.Equal(t, "HT20", info.HTMode)
	assert.Equal(t, "n", info.HWMode)
	assert.Equal(t, "ax/b/g/n", info.HWModesText)
	assert.Equal(t, []string{"b", "g", "n", "ax"}, info.HWModes)
	assert.Equal(t, []string{"HT20", "HT40", "HE20", "HE40"}, info.HTModes)

	// Hardware identification — the primary goal of this package.
	assert.Equal(t, "MediaTek MT7916AN", info.Hardware.Name)
	assert.Equal(t, []int{5315, 30982, 5315, 30982}, info.Hardware.ID)

	// Encryption
	assert.True(t, info.Encryption.Enabled)
	assert.Equal(t, []int{3}, info.Encryption.WPA)
	assert.Equal(t, []string{"sae"}, info.Encryption.Authentication)
	assert.Equal(t, []string{"ccmp"}, info.Encryption.Ciphers)
}

func TestGetInfoWithExecutor_Cypress_AllUnknownFields(t *testing.T) {
	mock := &mockUbusExecutor{output: readFixture(t, "info_cypress.json")}

	info, err := iwinfo.GetInfoWithExecutor(context.Background(), mock, "wlh-10-04")

	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, "phy3", info.PHY)
	assert.Equal(t, "2C:CF:67:BB:10:04", info.BSSID)
	assert.Equal(t, "Client", info.Mode)
	assert.Equal(t, 34, info.Channel)
	assert.Equal(t, 5170, info.Frequency)

	// Fields reported as "unknown" in iwinfo CLI are zero in the JSON.
	// These must not panic and must return the zero value.
	assert.Equal(t, 0, info.TxPower)
	assert.Equal(t, 0, info.Signal)
	assert.Equal(t, 0, info.Noise)
	assert.Equal(t, 0, info.Bitrate)

	assert.False(t, info.Encryption.Enabled)
	assert.Empty(t, info.Encryption.WPA)
	assert.Empty(t, info.Encryption.Ciphers)

	assert.Equal(t, "Cypress CYW43455", info.Hardware.Name)
	assert.Equal(t, []int{720, 43430, 0, 0}, info.Hardware.ID)
	assert.Equal(t, []string{"a", "b", "g", "n", "ac"}, info.HWModes)
}

func TestGetInfoWithExecutor_MorseMicro_HaLow(t *testing.T) {
	mock := &mockUbusExecutor{output: readFixture(t, "info_morsemicro.json")}

	info, err := iwinfo.GetInfoWithExecutor(context.Background(), mock, "wlh0")

	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, "phy0", info.PHY)
	assert.Equal(t, "halowmesh", info.SSID)
	assert.Equal(t, "F4:AB:5C:DF:3E:91", info.BSSID)
	assert.Equal(t, "Mesh Point", info.Mode)
	assert.Equal(t, 42, info.Channel)
	assert.Equal(t, 923, info.Frequency)
	assert.Equal(t, 27, info.TxPower)
	assert.Equal(t, 70, info.Quality)
	assert.Equal(t, 70, info.QualityMax)
	assert.Equal(t, -33, info.Signal)
	assert.Equal(t, -73, info.Noise)
	assert.Equal(t, 7500, info.Bitrate)
	assert.Equal(t, "ah", info.HWMode)
	assert.Equal(t, []string{"ah"}, info.HWModes)
	assert.Equal(t, []string{"HT20"}, info.HTModes)

	assert.True(t, info.Encryption.Enabled)
	assert.Equal(t, []int{3}, info.Encryption.WPA)
	assert.Equal(t, []string{"sae"}, info.Encryption.Authentication)

	// Embedded device: no PCI/USB IDs.
	assert.Equal(t, "Morse Micro SPI-MM601X", info.Hardware.Name)
	assert.Empty(t, info.Hardware.ID)
}

func TestGetInfoWithExecutor_UbusError(t *testing.T) {
	mock := &mockUbusExecutor{err: errors.New("no such device")}

	_, err := iwinfo.GetInfoWithExecutor(context.Background(), mock, "wlh0")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such device")
}

func TestGetInfoWithExecutor_MalformedJSON(t *testing.T) {
	mock := &mockUbusExecutor{output: []byte(`{bad json`)}

	_, err := iwinfo.GetInfoWithExecutor(context.Background(), mock, "wlh0")

	require.Error(t, err)
}

func TestGetInfoWithExecutor_EmptyObject(t *testing.T) {
	// Empty JSON object: all struct fields take zero values — must not panic.
	mock := &mockUbusExecutor{output: []byte(`{}`)}

	info, err := iwinfo.GetInfoWithExecutor(context.Background(), mock, "wlh0")

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "", info.Hardware.Name)
	assert.Equal(t, 0, info.Signal)
	assert.False(t, info.Encryption.Enabled)
}

// ── GetInfoForAllWithExecutor ─────────────────────────────────────────────────

func TestGetInfoForAllWithExecutor_AllThreeDevices(t *testing.T) {
	exec := &dispatchUbusExecutor{
		responses: map[string]mockResponse{
			"devices":                 {output: readFixture(t, "devices.json")},
			`{"device":"phy1-mesh0"}`: {output: readFixture(t, "info_mediatek.json")},
			`{"device":"wlh-10-04"}`:  {output: readFixture(t, "info_cypress.json")},
			`{"device":"wlh0"}`:       {output: readFixture(t, "info_morsemicro.json")},
		},
	}

	result, err := iwinfo.GetInfoForAllWithExecutor(context.Background(), exec)

	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, "MediaTek MT7916AN", result["phy1-mesh0"].Hardware.Name)
	assert.Equal(t, "Cypress CYW43455", result["wlh-10-04"].Hardware.Name)
	assert.Equal(t, "Morse Micro SPI-MM601X", result["wlh0"].Hardware.Name)
}

func TestGetInfoForAllWithExecutor_EmptyDeviceList(t *testing.T) {
	mock := &mockUbusExecutor{output: []byte(`{"devices":[]}`)}

	result, err := iwinfo.GetInfoForAllWithExecutor(context.Background(), mock)

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetInfoForAllWithExecutor_DevicesCallFails(t *testing.T) {
	mock := &mockUbusExecutor{err: errors.New("rpcd not running")}

	result, err := iwinfo.GetInfoForAllWithExecutor(context.Background(), mock)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rpcd not running")
	assert.Nil(t, result)
}

func TestGetInfoForAllWithExecutor_OneDeviceFails(t *testing.T) {
	exec := &dispatchUbusExecutor{
		responses: map[string]mockResponse{
			"devices":                 {output: readFixture(t, "devices.json")},
			`{"device":"phy1-mesh0"}`: {output: readFixture(t, "info_mediatek.json")},
			`{"device":"wlh-10-04"}`:  {err: errors.New("interface down")},
			`{"device":"wlh0"}`:       {err: errors.New("interface down")},
		},
	}

	result, err := iwinfo.GetInfoForAllWithExecutor(context.Background(), exec)

	// Partial failure: error is reported but successful devices are still returned.
	require.Error(t, err)
	assert.Contains(t, result, "phy1-mesh0")
	assert.NotContains(t, result, "wlh-10-04")
	assert.NotContains(t, result, "wlh0")
	assert.Equal(t, "MediaTek MT7916AN", result["phy1-mesh0"].Hardware.Name)
}

func TestGetInfoForAllWithExecutor_AllDevicesFail(t *testing.T) {
	exec := &dispatchUbusExecutor{
		responses: map[string]mockResponse{
			"devices": {output: readFixture(t, "devices.json")},
		},
		fallback: mockResponse{err: errors.New("interface unavailable")},
	}

	result, err := iwinfo.GetInfoForAllWithExecutor(context.Background(), exec)

	require.Error(t, err)
	// Map is non-nil but empty since all per-device calls failed.
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

// ── InterfaceInfo getter methods ──────────────────────────────────────────────

func TestInterfaceInfo_Getters(t *testing.T) {
	info := &iwinfo.InterfaceInfo{
		PHY:         "phy0",
		SSID:        "testnet",
		BSSID:       "AA:BB:CC:DD:EE:FF",
		Country:     "DE",
		Mode:        "Mesh Point",
		Channel:     6,
		CenterChan1: 6,
		CenterChan2: 0,
		Frequency:   2437,
		TxPower:     20,
		Quality:     55,
		QualityMax:  70,
		Signal:      -60,
		Noise:       -90,
		Bitrate:     54000,
		HTMode:      "HT40",
		HWMode:      "n",
		HWModesText: "ax/b/g/n",
		HWModes:     []string{"b", "g", "n", "ax"},
		HTModes:     []string{"HT20", "HT40"},
		Encryption: iwinfo.EncryptionInfo{
			Enabled:        true,
			WPA:            []int{2, 3},
			Authentication: []string{"psk2", "sae"},
			Ciphers:        []string{"ccmp"},
		},
		Hardware: iwinfo.HardwareInfo{
			ID:   []int{5315, 30982, 5315, 30982},
			Name: "MediaTek MT7916AN",
		},
	}

	assert.Equal(t, "phy0", info.GetPHY())
	assert.Equal(t, "testnet", info.GetSSID())
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", info.GetBSSID())
	assert.Equal(t, "DE", info.GetCountry())
	assert.Equal(t, "Mesh Point", info.GetMode())
	assert.Equal(t, 6, info.GetChannel())
	assert.Equal(t, 6, info.GetCenterChan1())
	assert.Equal(t, 0, info.GetCenterChan2())
	assert.Equal(t, 2437, info.GetFrequency())
	assert.Equal(t, 20, info.GetTxPower())
	assert.Equal(t, 55, info.GetQuality())
	assert.Equal(t, 70, info.GetQualityMax())
	assert.Equal(t, -60, info.GetSignal())
	assert.Equal(t, -90, info.GetNoise())
	assert.Equal(t, 54000, info.GetBitrate())
	assert.Equal(t, "HT40", info.GetHTMode())
	assert.Equal(t, "n", info.GetHWMode())
	assert.Equal(t, "ax/b/g/n", info.GetHWModesText())
	assert.Equal(t, []string{"b", "g", "n", "ax"}, info.GetHWModes())
	assert.Equal(t, []string{"HT20", "HT40"}, info.GetHTModes())
	assert.Equal(t, "MediaTek MT7916AN", info.GetHardwareName())
	assert.Equal(t, []int{5315, 30982, 5315, 30982}, info.GetHardwareID())
}

// ── EncryptionInfo ────────────────────────────────────────────────────────────

func TestEncryptionInfo_WPA3_SAE(t *testing.T) {
	enc := iwinfo.EncryptionInfo{
		Enabled:        true,
		WPA:            []int{3},
		Authentication: []string{"sae"},
		Ciphers:        []string{"ccmp"},
	}

	assert.True(t, enc.IsEnabled())
	assert.Equal(t, []int{3}, enc.GetWPA())
	assert.Equal(t, []string{"sae"}, enc.GetAuthentication())
	assert.Equal(t, []string{"ccmp"}, enc.GetCiphers())
}

func TestEncryptionInfo_WPA2_WPA3_Mixed(t *testing.T) {
	enc := iwinfo.EncryptionInfo{
		Enabled:        true,
		WPA:            []int{2, 3},
		Authentication: []string{"psk2", "sae"},
		Ciphers:        []string{"ccmp"},
	}

	assert.True(t, enc.IsEnabled())
	assert.Equal(t, []int{2, 3}, enc.GetWPA())
	assert.Equal(t, []string{"psk2", "sae"}, enc.GetAuthentication())
}

func TestEncryptionInfo_Disabled(t *testing.T) {
	enc := iwinfo.EncryptionInfo{Enabled: false}

	assert.False(t, enc.IsEnabled())
	assert.Empty(t, enc.GetWPA())
	assert.Empty(t, enc.GetAuthentication())
	assert.Empty(t, enc.GetCiphers())
}

// ── HardwareInfo ──────────────────────────────────────────────────────────────

func TestHardwareInfo_Getters(t *testing.T) {
	hw := iwinfo.HardwareInfo{
		ID:   []int{5315, 30982, 5315, 30982},
		Name: "MediaTek MT7916AN",
	}

	assert.Equal(t, "MediaTek MT7916AN", hw.GetName())
	assert.Equal(t, []int{5315, 30982, 5315, 30982}, hw.GetID())
}

func TestHardwareInfo_EmbeddedDevice_NoID(t *testing.T) {
	hw := iwinfo.HardwareInfo{
		ID:   []int{},
		Name: "Morse Micro SPI-MM601X",
	}

	assert.Equal(t, "Morse Micro SPI-MM601X", hw.GetName())
	assert.Empty(t, hw.GetID())
}

// ── ubus argument capture ─────────────────────────────────────────────────────

func TestUbusArgs_GetDevices(t *testing.T) {
	exec := &dispatchUbusExecutor{
		fallback: mockResponse{output: []byte(`{"devices":[]}`)},
	}

	_, err := iwinfo.GetDevicesWithExecutor(context.Background(), exec)

	require.NoError(t, err)
	require.Len(t, exec.calls, 1)
	assert.Equal(t, []string{"call", "iwinfo", "devices"}, exec.calls[0])
}

func TestUbusArgs_GetInfo(t *testing.T) {
	exec := &dispatchUbusExecutor{
		fallback: mockResponse{output: []byte(`{}`)},
	}

	_, err := iwinfo.GetInfoWithExecutor(context.Background(), exec, "wlh0")

	require.NoError(t, err)
	require.Len(t, exec.calls, 1)
	assert.Equal(t, []string{"call", "iwinfo", "info", `{"device":"wlh0"}`}, exec.calls[0])
}

func TestUbusArgs_GetInfo_HyphenatedName(t *testing.T) {
	exec := &dispatchUbusExecutor{
		fallback: mockResponse{output: []byte(`{}`)},
	}

	_, err := iwinfo.GetInfoWithExecutor(context.Background(), exec, "phy1-mesh0")

	require.NoError(t, err)
	require.Len(t, exec.calls, 1)
	assert.Equal(t, []string{"call", "iwinfo", "info", `{"device":"phy1-mesh0"}`}, exec.calls[0])
}

func TestUbusArgs_GetInfoForAll_CallSequence(t *testing.T) {
	exec := &dispatchUbusExecutor{
		responses: map[string]mockResponse{
			"devices":           {output: []byte(`{"devices":["wlh0"]}`)},
			`{"device":"wlh0"}`: {output: readFixture(t, "info_morsemicro.json")},
		},
	}

	_, err := iwinfo.GetInfoForAllWithExecutor(context.Background(), exec)

	require.NoError(t, err)
	// First call: devices list; second call: per-device info.
	require.Len(t, exec.calls, 2)
	assert.Equal(t, []string{"call", "iwinfo", "devices"}, exec.calls[0])
	assert.Equal(t, []string{"call", "iwinfo", "info", `{"device":"wlh0"}`}, exec.calls[1])
}

// ── Client ────────────────────────────────────────────────────────────────────

func TestNewClientWithExecutor_ImplementsProvider(t *testing.T) {
	// Compile-time check that *Client satisfies IwinfoProvider.
	var _ iwinfo.IwinfoProvider = iwinfo.NewClientWithExecutor(&mockUbusExecutor{})
}

func TestClient_GetDevices(t *testing.T) {
	mock := &mockUbusExecutor{output: readFixture(t, "devices.json")}
	client := iwinfo.NewClientWithExecutor(mock)

	devices, err := client.GetDevices(context.Background())

	require.NoError(t, err)
	assert.Len(t, devices, 3)
}

func TestClient_GetInfo(t *testing.T) {
	mock := &mockUbusExecutor{output: readFixture(t, "info_mediatek.json")}
	client := iwinfo.NewClientWithExecutor(mock)

	info, err := client.GetInfo(context.Background(), "phy1-mesh0")

	require.NoError(t, err)
	assert.Equal(t, "MediaTek MT7916AN", info.GetHardwareName())
}

func TestClient_GetInfoForAll(t *testing.T) {
	exec := &dispatchUbusExecutor{
		responses: map[string]mockResponse{
			"devices":                 {output: readFixture(t, "devices.json")},
			`{"device":"phy1-mesh0"}`: {output: readFixture(t, "info_mediatek.json")},
			`{"device":"wlh-10-04"}`:  {output: readFixture(t, "info_cypress.json")},
			`{"device":"wlh0"}`:       {output: readFixture(t, "info_morsemicro.json")},
		},
	}
	client := iwinfo.NewClientWithExecutor(exec)

	result, err := client.GetInfoForAll(context.Background())

	require.NoError(t, err)
	assert.Len(t, result, 3)
}
