package handlers_test

import (
	"context"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/digineo/go-uci/v2"
	setupv1 "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1"
	wificonfigv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── helper: build a SetupService over a populated UCI tree ──────────────────

// newSetupReader returns a fakeConfigReader populated with a baseline
// reset-OpenMANET wireless tree (one mac80211 + one morse radio) so
// most tests don't have to repeat the setup boilerplate.
func newSetupReader() *fakeConfigReader {
	return &fakeConfigReader{
		data: map[string]map[string]map[string][]string{
			"wireless": {
				"radio0": {
					"type":    {"mac80211"},
					"band":    {"2g"},
					"channel": {"1"},
					"path":    {"platform/soc/fe980000.usb/usb1/1-1"},
				},
				"radio1": {
					"type":    {"morse"},
					"band":    {"s1g"},
					"channel": {"42"},
					"path":    {"platform/soc/fe204000.spi"},
				},
			},
			"system": {
				"@system[0]": {
					"hostname": {"BCM2711-97d6"},
					"timezone": {"UTC"},
				},
			},
		},
		sectionTypes: map[string]map[string]string{
			"wireless": {
				"radio0": "wifi-device",
				"radio1": "wifi-device",
			},
			"system": {
				"@system[0]": "system",
			},
		},
	}
}

func newSetupService(t *testing.T, cfg *config.Config, reader *fakeConfigReader, ifaces *fakeInterfaceProvider) *handlers.SetupService {
	t.Helper()

	return &handlers.SetupService{
		Cfg:        cfg,
		Log:        zerolog.Nop(),
		UCI:        reader,
		Interfaces: ifaces,
	}
}

// ── Translator round-trip tests ─────────────────────────────────────────────

func TestProtoToMeshRole_RoundTrip(t *testing.T) {
	cases := []struct {
		role setupv1.MeshRole
		uci  string
	}{
		{setupv1.MeshRole_MESH_ROLE_MESH_POINT, "0"},
		{setupv1.MeshRole_MESH_ROLE_MESH_GATE, "1"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.uci, handlers.ProtoToMeshRole(tc.role))
		assert.Equal(t, tc.role, handlers.MeshRoleToProto(tc.uci))
	}

	assert.Equal(t, "", handlers.ProtoToMeshRole(setupv1.MeshRole_MESH_ROLE_UNSPECIFIED))
	assert.Equal(t, setupv1.MeshRole_MESH_ROLE_UNSPECIFIED, handlers.MeshRoleToProto("nonsense"))
}

func TestProtoToMeshPointMode_RoundTrip(t *testing.T) {
	cases := []struct {
		mode setupv1.MeshPointMode
		uci  string
	}{
		{setupv1.MeshPointMode_MESH_POINT_MODE_NONE, "none"},
		{setupv1.MeshPointMode_MESH_POINT_MODE_EXTENDER, "extender"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.uci, handlers.ProtoToMeshPointMode(tc.mode))
		assert.Equal(t, tc.mode, handlers.MeshPointModeToProto(tc.uci))
	}

	assert.Equal(t, setupv1.MeshPointMode_MESH_POINT_MODE_UNSPECIFIED, handlers.MeshPointModeToProto("bridge"))
}

func TestProtoToMeshGateMode_RoundTrip(t *testing.T) {
	cases := []struct {
		mode setupv1.MeshGateMode
		uci  string
	}{
		{setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER, "router"},
		{setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER_FIREWALL, "router_firewall"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.uci, handlers.ProtoToMeshGateMode(tc.mode))
		assert.Equal(t, tc.mode, handlers.MeshGateModeToProto(tc.uci))
	}

	assert.Equal(t, setupv1.MeshGateMode_MESH_GATE_MODE_UNSPECIFIED, handlers.MeshGateModeToProto("bridge"))
}

func TestProtoToUplinkType_RoundTrip(t *testing.T) {
	cases := []struct {
		typ setupv1.UplinkType
		uci string
	}{
		{setupv1.UplinkType_UPLINK_TYPE_ETHERNET, "ethernet"},
		{setupv1.UplinkType_UPLINK_TYPE_WIRELESS_STA, "wifi-sta"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.uci, handlers.ProtoToUplinkType(tc.typ))
		assert.Equal(t, tc.typ, handlers.UplinkTypeToProto(tc.uci))
	}

	// `wireless-sta` is also accepted on the inbound side as a
	// tolerant alias.
	assert.Equal(t, setupv1.UplinkType_UPLINK_TYPE_WIRELESS_STA, handlers.UplinkTypeToProto("wireless-sta"))

	assert.Equal(t, setupv1.UplinkType_UPLINK_TYPE_UNSPECIFIED, handlers.UplinkTypeToProto("none"))
}

// ── GetSetupStatus ──────────────────────────────────────────────────────────

func TestGetSetupStatus_FreshDevice_DefaultsDisabled(t *testing.T) {
	// auth.enable is explicitly false to model a true factory image
	// before the wizard has run. DefaultAuthEnable is true at the
	// firmware level (set by /etc/openmanetd/config.yml), but a brand-
	// new factory image flips it off until the wizard finishes.
	cfg := setupBLOSTestConfig(t, "auth:\n  enable: false\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.False(t, resp.GetIsEnabled(), "default setup.enabled is false")
	assert.False(t, resp.GetIsSetupComplete())
	assert.True(t, resp.GetHasHalowRadio(), "morse radio in fixture should set HasHalowRadio")
	assert.False(t, resp.GetAlreadyConfigured(), "factory hostname should not flag already configured")
	assert.Equal(t, "BCM2711-97d6", resp.GetCurrentHostname())
	assert.Len(t, resp.GetRadios(), 2)
}

// TestGetSetupStatus_ReturnsTimezones asserts the response carries the
// full, sorted tzinfo table plus the device's currently-configured
// zonename (read from system.@system[0].zonename).
func TestGetSetupStatus_ReturnsTimezones(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "auth:\n  enable: false\n")

	reader := newSetupReader()
	reader.data["system"]["@system[0]"]["zonename"] = []string{"America/Denver"}

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.Equal(t, "America/Denver", resp.GetCurrentTimezone())

	tzs := resp.GetTimezones()
	require.NotEmpty(t, tzs)
	assert.True(t, sort.StringsAreSorted(tzs), "Timezones must be sorted")
	assert.Contains(t, tzs, "America/Denver")
}

// TestGetSetupStatus_CurrentTimezoneEmptyWhenUnset asserts a fresh
// device (no zonename ever written) reports an empty current_timezone
// rather than erroring.
func TestGetSetupStatus_CurrentTimezoneEmptyWhenUnset(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "auth:\n  enable: false\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.Empty(t, resp.GetCurrentTimezone())
}

func TestGetSetupStatus_EnabledIncomplete(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n  complete: false\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetIsEnabled())
	assert.False(t, resp.GetIsSetupComplete())
}

func TestGetSetupStatus_EnabledComplete(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n  complete: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetIsEnabled())
	assert.True(t, resp.GetIsSetupComplete())
}

func TestGetSetupStatus_NoHalowRadio(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")

	reader := &fakeConfigReader{
		data: map[string]map[string]map[string][]string{
			"wireless": {
				"radio0": {"type": {"mac80211"}, "band": {"2g"}},
			},
		},
		sectionTypes: map[string]map[string]string{
			"wireless": {"radio0": "wifi-device"},
		},
	}

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.False(t, resp.GetHasHalowRadio())
	assert.Len(t, resp.GetRadios(), 1)
	assert.False(t, resp.GetRadios()[0].GetIsHalow())
}

func TestGetSetupStatus_RadioMetadataIsHalowSet(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	radios := resp.GetRadios()
	require.Len(t, radios, 2)

	byName := map[string]*setupv1.SetupRadio{}
	for _, r := range radios {
		byName[r.GetName()] = r
	}

	require.NotNil(t, byName["radio0"])
	assert.False(t, byName["radio0"].GetIsHalow())
	assert.Equal(t, "2g", byName["radio0"].GetBand())

	require.NotNil(t, byName["radio1"])
	assert.True(t, byName["radio1"].GetIsHalow())
	assert.Equal(t, "s1g", byName["radio1"].GetBand())
}

func TestGetSetupStatus_AlreadyConfigured_NonFactoryHostname(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")

	reader := newSetupReader()
	reader.data["system"]["@system[0]"]["hostname"] = []string{"my-mesh-node-1"}

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetAlreadyConfigured(), "non-factory hostname must flag already_configured")
}

func TestGetSetupStatus_AlreadyConfigured_AuthEnabled(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "auth:\n  enable: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetAlreadyConfigured(), "auth.enable=true must flag already_configured")
}

func TestGetSetupStatus_AlreadyConfigured_FactoryMeshGateAnnouncementsIgnored(t *testing.T) {
	// Factory firmware ships with `mesh_gate_announcements '0'` already
	// set in /etc/config/mesh11sd. That MUST NOT trip the
	// already_configured heuristic — otherwise every fresh device sees
	// the "looks already configured" warning on first boot.
	//
	// auth.enable is explicitly disabled here so the test isolates the
	// mesh11sd heuristic from the auth-enable heuristic (which would
	// otherwise dominate given DefaultAuthEnable=true).
	cfg := setupBLOSTestConfig(t, "auth:\n  enable: false\n")

	reader := newSetupReader()
	if reader.data["mesh11sd"] == nil {
		reader.data["mesh11sd"] = map[string]map[string][]string{}
	}

	reader.data["mesh11sd"]["mesh_params"] = map[string][]string{
		"mesh_gate_announcements": {"0"},
	}

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.False(t, resp.GetAlreadyConfigured(),
		"factory mesh_gate_announcements='0' must not flag already_configured")
}

func TestGetSetupStatus_AlreadyConfigured_WizardBookkeepingSection(t *testing.T) {
	// The wizard writes a `config wizard 'wizard'` section into
	// /etc/config/network on apply (writeWizardBookkeeping; pinned by
	// TestCompat_WizardBookkeepingSectionType). If we see one, the
	// wizard has already run.
	cfg := setupBLOSTestConfig(t, "")

	reader := newSetupReader()
	require.NoError(t, reader.AddSection("network", "wizard", "wizard"))

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetAlreadyConfigured())
}

func TestGetSetupStatus_AlreadyConfigured_AhwlanInterfacePresent(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")

	reader := newSetupReader()
	require.NoError(t, reader.AddSection("network", "ahwlan", "interface"))

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetAlreadyConfigured())
}

func TestGetSetupStatus_EthernetPortsFiltered(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")

	ifaces := &fakeInterfaceProvider{
		infos: []network.NetworkInterfaceInfo{
			{Name: "eth0"},
			{Name: "eth1"},
			{Name: "wlan0"},      // filtered
			{Name: "wlh0"},       // filtered (Morse HaLow)
			{Name: "br-lan"},     // filtered
			{Name: "lo"},         // filtered
			{Name: "tailscale0"}, // filtered
			{Name: "bat0"},       // filtered
		},
	}

	svc := newSetupService(t, cfg, newSetupReader(), ifaces)

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.Equal(t, []string{"eth0", "eth1"}, resp.GetEthernetPorts())
}

// ── Regulatory database integration ───────────────────────────────────────

// regdbFixturePath locates testfixtures/setup-wizard/channels.csv from
// the handlers test package.
func regdbFixturePath(t *testing.T) string {
	t.Helper()

	_, here, _, ok := runtime.Caller(0)
	require.True(t, ok)

	root := here
	for range 5 {
		root = filepath.Dir(root)
	}

	return filepath.Join(root, "testfixtures", "setup-wizard", "channels.csv")
}

func TestGetSetupStatus_PopulatesCountriesFromRegDB(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")

	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})
	svc.RegDBPath = regdbFixturePath(t)

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	require.NotEmpty(t, resp.GetCountries(), "regdb fixture must produce countries")

	codes := make(map[string]bool, len(resp.GetCountries()))
	for _, c := range resp.GetCountries() {
		codes[c.GetCode()] = true
	}

	for _, expected := range []string{"US", "GB", "JP"} {
		assert.True(t, codes[expected], "country %q must be present", expected)
	}

	// Spot-check that US has the expected 8 MHz allocation.
	for _, c := range resp.GetCountries() {
		if c.GetCode() != "US" {
			continue
		}

		var got []uint32

		for _, b := range c.GetBandwidths() {
			if b.GetMhz() == 8 {
				got = b.GetChannels()
			}
		}

		assert.Equal(t, []uint32{12, 28, 44}, got)

		break
	}
}

func TestGetSetupStatus_CurrentCountryFromMorseRadio(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")

	reader := newSetupReader()
	reader.data["wireless"]["radio1"]["country"] = []string{"US"}

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})
	svc.RegDBPath = regdbFixturePath(t)

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.Equal(t, "US", resp.GetCurrentCountry())
}

func TestGetSetupStatus_RegDBMissing_ReturnsEmptyCountriesNoError(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")

	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})
	svc.RegDBPath = filepath.Join(t.TempDir(), "nope.csv")

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err, "missing regdb is a soft failure: GetSetupStatus must succeed")

	assert.Empty(t, resp.GetCountries(), "no regdb means no countries to advertise")
}

// ── ApplySetup guards ──────────────────────────────────────────────────────

// streamCollector implements handlers.ApplySetupStream by appending
// every event to an in-memory slice. Tests inspect the slice to
// verify the per-phase event sequence (STARTED → DONE | FAILED).
type streamCollector struct {
	sent []*setupv1.ApplySetupResponse
}

func (c *streamCollector) Send(msg *setupv1.ApplySetupResponse) error {
	c.sent = append(c.sent, msg)

	return nil
}

func TestApplySetup_RejectsWhenSetupDisabled(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: false\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), &streamCollector{})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnavailable, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "disabled")
}

func TestApplySetup_RejectsWhenAlreadyComplete(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n  complete: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), &streamCollector{})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "already completed")
}

func TestApplySetup_RejectsWhenLegacyLuciWizardAlreadyRan(t *testing.T) {
	// Devices that previously went through the legacy LuCI Morse
	// wizard write `luci.wizard.used='1'`. Those devices already have
	// the same end-state UCI the new wizard would produce; running
	// the new wizard would just reset everything and confuse the
	// operator. Reject ApplySetup with CodeFailedPrecondition.
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")

	reader := newSetupReader()
	require.NoError(t, reader.AddSection("luci", "wizard", "wizard"))
	require.NoError(t, reader.SetType("luci", "wizard", "used", uci.TypeOption, "1"))

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), &streamCollector{})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "legacy LuCI Morse wizard")
}

func TestGetSetupStatus_LegacyLuciWizardUsedFlagsSetupComplete(t *testing.T) {
	// luci.wizard.used=1 is treated as is_setup_complete=true so the
	// frontend redirects /setup → / (the wizard route is hidden).
	cfg := setupBLOSTestConfig(t, "auth:\n  enable: false\n")

	reader := newSetupReader()
	require.NoError(t, reader.AddSection("luci", "wizard", "wizard"))
	require.NoError(t, reader.SetType("luci", "wizard", "used", uci.TypeOption, "1"))

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetIsSetupComplete(),
		"luci.wizard.used=1 must surface as is_setup_complete=true")
}

func TestGetSetupStatus_LegacyLuciWizardZeroDoesNotFlag(t *testing.T) {
	// luci.wizard.used=0 means "wizard not yet run" (just present in
	// the config skeleton). It must not flag setup as complete.
	cfg := setupBLOSTestConfig(t, "auth:\n  enable: false\n")

	reader := newSetupReader()
	require.NoError(t, reader.AddSection("luci", "wizard", "wizard"))
	require.NoError(t, reader.SetType("luci", "wizard", "used", uci.TypeOption, "0"))

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.False(t, resp.GetIsSetupComplete(),
		"luci.wizard.used=0 must NOT flag setup complete")
}

func TestApplySetup_RejectsWhenNoHalowRadio(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")

	reader := &fakeConfigReader{
		data: map[string]map[string]map[string][]string{
			"wireless": {
				"radio0": {"type": {"mac80211"}},
			},
		},
		sectionTypes: map[string]map[string]string{
			"wireless": {"radio0": "wifi-device"},
		},
	}

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), &streamCollector{})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
	assert.Contains(t, connectErr.Message(), "HaLow")
}

func TestApplySetup_RejectsNilProfile(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	err := svc.ApplySetupForTest(context.Background(), nil, &streamCollector{})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

// ── ApplySetup validation phase ────────────────────────────────────────────

func TestApplySetup_RegDBPresent_RejectsIllegalCountryChannelTuple(t *testing.T) {
	// US allows channel 42 at 2 MHz but not at 8 MHz. Confirm the
	// handler refuses the latter when the regdb is loaded.
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})
	svc.RegDBPath = regdbFixturePath(t)

	prof := minimalProfile()
	prof.Mesh.CountryCode = "US"
	prof.Mesh.BandwidthMhz = 8
	prof.Mesh.Channel = 42 // legal at 2 MHz, NOT at 8 MHz

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RegDBPresent_RejectsEmptyCountry(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})
	svc.RegDBPath = regdbFixturePath(t)

	prof := minimalProfile()
	prof.Mesh.CountryCode = "" // unset by the user

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RegDBPresent_AcceptsLegalTuple(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})
	svc.RegDBPath = regdbFixturePath(t)

	prof := minimalProfile()
	prof.Mesh.CountryCode = "US"
	prof.Mesh.BandwidthMhz = 2
	prof.Mesh.Channel = 42

	// validation should accept; the apply will fail later (missing
	// password setter etc.), but NOT with InvalidArgument.
	err := runApplySetup(t, svc, prof)
	if err != nil {
		var ce *connect.Error
		require.ErrorAs(t, err, &ce)
		assert.NotEqual(t, connect.CodeInvalidArgument, ce.Code(),
			"a legal country/channel/bandwidth tuple must not be rejected at validation")
	}
}

func TestApplySetup_RejectsInvalidHostname(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Hostname = "-leading-dash"

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsTrailingDashHostname(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Hostname = "trailing-dash-"

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsRoleUnspecified(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Role = setupv1.MeshRole_MESH_ROLE_UNSPECIFIED

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsMeshGateWithoutMode(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Role = setupv1.MeshRole_MESH_ROLE_MESH_GATE
	prof.DeviceMode = nil // no meshgate_mode set

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsMeshGateWithoutUplink(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Role = setupv1.MeshRole_MESH_ROLE_MESH_GATE
	prof.DeviceMode = &setupv1.MeshNodeProfile_MeshgateMode{
		MeshgateMode: setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER,
	}
	prof.Uplink = nil

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsMeshRadioNotMorse(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Mesh.RadioName = "radio0" // mac80211, not morse

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsAPSameAsMeshRadio(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Aps = []*setupv1.RadioApProfile{
		{
			RadioName:  "radio1", // same as mesh radio
			Enabled:    true,
			Ssid:       "test",
			Passphrase: "longenough",
			Encryption: wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_SAE,
		},
	}

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsDuplicateAPRadios(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Aps = []*setupv1.RadioApProfile{
		{RadioName: "radio0", Enabled: true, Encryption: wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_NONE},
		{RadioName: "radio0", Enabled: true, Encryption: wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_NONE},
	}

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsShortPassphrase(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Mesh.Passphrase = "short" // 5 chars, encryption is SAE

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsShortAdminPassword(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.AdminPassword = "short"

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_RejectsIllegalChannel(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Mesh.Channel = 0 // invalid

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

func TestApplySetup_ValidProfileEmitsValidateStartedAndDone(t *testing.T) {
	// A valid profile + minimal SetupService (no Snapshotter, no
	// PasswordSetter) drives phases 1-12 to DONE then fails phase
	// 13 (PasswordSetter not configured). Asserts the validate
	// phase emitted STARTED + DONE events.
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	collector := &streamCollector{}
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code(),
		"missing PasswordSetter surfaces as CodeInternal at phase 13")

	require.NotEmpty(t, collector.sent)

	// First event must be VALIDATE STARTED.
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_VALIDATE, collector.sent[0].GetPhase())
	assert.Equal(t, setupv1.ApplySetupResponse_STATUS_STARTED, collector.sent[0].GetStatus())

	// VALIDATE DONE must be the second event.
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_VALIDATE, collector.sent[1].GetPhase())
	assert.Equal(t, setupv1.ApplySetupResponse_STATUS_DONE, collector.sent[1].GetStatus())

	// Each STARTED event for a non-terminal phase must be paired
	// with a subsequent DONE or FAILED for the same phase.
	pairsSeen := map[setupv1.ApplySetupResponse_Phase]int{}

	for _, ev := range collector.sent {
		if ev.GetPhase() == setupv1.ApplySetupResponse_PHASE_TERMINAL {
			continue
		}

		switch ev.GetStatus() {
		case setupv1.ApplySetupResponse_STATUS_STARTED:
			pairsSeen[ev.GetPhase()]++
		case setupv1.ApplySetupResponse_STATUS_DONE,
			setupv1.ApplySetupResponse_STATUS_FAILED:
			pairsSeen[ev.GetPhase()]--
		}
	}

	for p, n := range pairsSeen {
		assert.Equalf(t, 0, n, "phase %v had unbalanced STARTED/DONE|FAILED events (%d)", p, n)
	}
}

func TestApplySetup_InvalidProfileEmitsValidateFailedAndTerminal(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	collector := &streamCollector{}
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	prof := minimalProfile()
	prof.Hostname = "-bad"

	err := svc.ApplySetupForTest(context.Background(), prof, collector)
	require.Error(t, err)

	require.GreaterOrEqual(t, len(collector.sent), 3, "STARTED + FAILED + TERMINAL")

	// First event is STARTED on PHASE_VALIDATE.
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_VALIDATE, collector.sent[0].GetPhase())
	assert.Equal(t, setupv1.ApplySetupResponse_STATUS_STARTED, collector.sent[0].GetStatus())

	// Second event is FAILED on PHASE_VALIDATE.
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_VALIDATE, collector.sent[1].GetPhase())
	assert.Equal(t, setupv1.ApplySetupResponse_STATUS_FAILED, collector.sent[1].GetStatus())

	// Last event is the terminal failure carrying ApplySetupResult.
	terminal := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, terminal.GetPhase())
	assert.Equal(t, setupv1.ApplySetupResponse_STATUS_FAILED, terminal.GetStatus())

	require.NotNil(t, terminal.GetResult())
	assert.False(t, terminal.GetResult().GetSuccess())
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_VALIDATE,
		terminal.GetResult().GetFailedPhase())
}

// ── helpers ────────────────────────────────────────────────────────────────

// minimalProfile returns a fully-valid MeshNodeProfile that the
// validation phase accepts. Tests mutate one field at a time to
// exercise specific validation failures.
func minimalProfile() *setupv1.MeshNodeProfile {
	return &setupv1.MeshNodeProfile{
		Hostname:      "openmanet-1",
		AdminPassword: "supersecret",
		Role:          setupv1.MeshRole_MESH_ROLE_MESH_POINT,
		DeviceMode: &setupv1.MeshNodeProfile_MeshpointMode{
			MeshpointMode: setupv1.MeshPointMode_MESH_POINT_MODE_EXTENDER,
		},
		Mesh: &setupv1.MeshRadioConfig{
			RadioName:    "radio1",
			MeshId:       "openmanet-mesh",
			Passphrase:   "longpasscode",
			Encryption:   wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_SAE,
			BandwidthMhz: 2,
			Channel:      42,
		},
		Aps: []*setupv1.RadioApProfile{},
	}
}

func runApplySetup(t *testing.T, svc *handlers.SetupService, profile *setupv1.MeshNodeProfile) error {
	t.Helper()

	return svc.ApplySetupForTest(context.Background(), profile, &streamCollector{})
}

func requireConnectCode(t *testing.T, err error, want connect.Code) {
	t.Helper()
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, want, connectErr.Code())
}

// ── Phase 2-12 scenario tests ───────────────────────────────────────────────

// fakeUCISnapshot is a simple test double for UCISnapshot. It just
// remembers which configs it was created from so tests can assert
// the snapshotter saw the expected scopes.
type fakeUCISnapshot struct {
	configs []string
}

func (f *fakeUCISnapshot) Configs() []string { return f.configs }

// fakeSnapshotter implements UCISnapshotter and records the calls
// made to it. Restore is a no-op.
type fakeSnapshotter struct {
	snapshotCalls int
	restoreCalls  int
	snapshotErr   error
	restoreErr    error
	lastSnapshot  *fakeUCISnapshot
}

func (f *fakeSnapshotter) Snapshot(_ context.Context, configs []string) (handlers.UCISnapshot, error) {
	f.snapshotCalls++

	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}

	f.lastSnapshot = &fakeUCISnapshot{configs: append([]string(nil), configs...)}

	return f.lastSnapshot, nil
}

func (f *fakeSnapshotter) Restore(_ context.Context, _ handlers.UCISnapshot) error {
	f.restoreCalls++

	return f.restoreErr
}

// fakeHostnameSetter records the hostnames it was asked to set.
type fakeHostnameSetter struct {
	calls []string
	err   error
}

func (f *fakeHostnameSetter) SetHostname(_ context.Context, hostname string) error {
	f.calls = append(f.calls, hostname)

	return f.err
}

// fakePasswordSetter records SetPassword calls so tests can assert
// the wizard wrote the admin password exactly once with the right
// arguments.
type fakePasswordSetter struct {
	mu    sync.Mutex
	calls []passwordCall
	err   error
}

type passwordCall struct {
	username string
	password string
}

func (f *fakePasswordSetter) SetPassword(_ context.Context, username, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, passwordCall{username: username, password: password})

	return f.err
}

func (f *fakePasswordSetter) callsCopy() []passwordCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]passwordCall(nil), f.calls...)
}

// fakeServiceReloader records Reload/Restart calls so tests can
// verify the post-terminal goroutine fired against the expected
// init.d service list.
type fakeServiceReloader struct {
	mu           sync.Mutex
	reloadCalls  []string
	restartCalls []string
	reloadErr    error
	restartErr   error
	done         chan struct{}
}

func newFakeReloader(expectedReloads int) *fakeServiceReloader {
	return &fakeServiceReloader{done: make(chan struct{}, expectedReloads)}
}

func (f *fakeServiceReloader) Reload(_ context.Context, service string) error {
	f.mu.Lock()
	f.reloadCalls = append(f.reloadCalls, service)
	err := f.reloadErr
	f.mu.Unlock()

	if err == nil {
		select {
		case f.done <- struct{}{}:
		default:
		}
	}

	return err
}

func (f *fakeServiceReloader) Restart(_ context.Context, service string) error {
	f.mu.Lock()
	f.restartCalls = append(f.restartCalls, service)
	err := f.restartErr
	f.mu.Unlock()

	if err == nil {
		select {
		case f.done <- struct{}{}:
		default:
		}
	}

	return err
}

func (f *fakeServiceReloader) reloadCallsCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.reloadCalls...)
}

func (f *fakeServiceReloader) restartCallsCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.restartCalls...)
}

// newFullSetupReader returns a fakeConfigReader populated with a
// reset-baseline mirror of the captured before/* fixtures (a single
// anonymous dnsmasq, lan/wan zones, lan/wan dhcp pools, mesh11sd
// sections, system section). The mock is rich enough to drive
// phases 2-12 to completion on a happy-path mesh-point profile.
func newFullSetupReader() *fakeConfigReader {
	r := newSetupReader()

	// dnsmasq + dhcp pools
	if r.data["dhcp"] == nil {
		r.data["dhcp"] = map[string]map[string][]string{}
	}

	r.data["dhcp"]["@dnsmasq[0]"] = map[string][]string{
		"domainneeded": {"1"},
		"local":        {"/lan/"},
		"domain":       {"lan"},
	}
	r.data["dhcp"]["lan"] = map[string][]string{
		"interface": {"lan"},
		"start":     {"100"},
		"limit":     {"150"},
		"leasetime": {"12h"},
	}
	r.data["dhcp"]["wan"] = map[string][]string{
		"interface": {"wan"},
		"ignore":    {"1"},
	}

	if r.sectionTypes["dhcp"] == nil {
		r.sectionTypes["dhcp"] = map[string]string{}
	}

	r.sectionTypes["dhcp"]["@dnsmasq[0]"] = "dnsmasq"
	r.sectionTypes["dhcp"]["lan"] = "dhcp"
	r.sectionTypes["dhcp"]["wan"] = "dhcp"

	// firewall: lan + wan zones, default forwarding, no rules.
	if r.data["firewall"] == nil {
		r.data["firewall"] = map[string]map[string][]string{}
	}

	r.data["firewall"]["@zone[0]"] = map[string][]string{
		"name":    {"lan"},
		"network": {"lan"},
		"input":   {"ACCEPT"},
		"output":  {"ACCEPT"},
		"forward": {"ACCEPT"},
	}
	r.data["firewall"]["@zone[1]"] = map[string][]string{
		"name":    {"wan"},
		"network": {"wan"},
		"input":   {"ACCEPT"},
		"output":  {"ACCEPT"},
		"forward": {"ACCEPT"},
	}

	if r.sectionTypes["firewall"] == nil {
		r.sectionTypes["firewall"] = map[string]string{}
	}

	r.sectionTypes["firewall"]["@zone[0]"] = "zone"
	r.sectionTypes["firewall"]["@zone[1]"] = "zone"

	// mesh11sd: setup + mesh_params + a few decorative sections.
	if r.data["mesh11sd"] == nil {
		r.data["mesh11sd"] = map[string]map[string][]string{}
	}

	r.data["mesh11sd"]["setup"] = map[string][]string{"enabled": {"0"}}
	r.data["mesh11sd"]["mesh_params"] = map[string][]string{
		"mesh_fwding":             {"1"},
		"mesh_gate_announcements": {"0"},
	}

	if r.sectionTypes["mesh11sd"] == nil {
		r.sectionTypes["mesh11sd"] = map[string]string{}
	}

	r.sectionTypes["mesh11sd"]["setup"] = "mesh11sd"
	r.sectionTypes["mesh11sd"]["mesh_params"] = "mesh11sd"

	// network: loopback + lan + wan + ahwlan empty interface.
	if r.data["network"] == nil {
		r.data["network"] = map[string]map[string][]string{}
	}

	r.data["network"]["loopback"] = map[string][]string{"device": {"lo"}, "proto": {"static"}}
	r.data["network"]["lan"] = map[string][]string{"proto": {"static"}, "ipaddr": {"10.41.254.1"}}
	r.data["network"]["wan"] = map[string][]string{"proto": {"dhcp"}}

	if r.sectionTypes["network"] == nil {
		r.sectionTypes["network"] = map[string]string{}
	}

	r.sectionTypes["network"]["loopback"] = "interface"
	r.sectionTypes["network"]["lan"] = "interface"
	r.sectionTypes["network"]["wan"] = "interface"

	return r
}

// fullDeps bundles the fake dependencies returned by
// newFullSetupService so tests can inspect each one without long
// argument lists.
type fullDeps struct {
	Snap     *fakeSnapshotter
	Host     *fakeHostnameSetter
	Pass     *fakePasswordSetter
	Reloader *fakeServiceReloader
}

// newFullSetupService builds a SetupService over a full reader plus
// fake snapshotter, hostname setter, password setter, and reloader,
// so phase tests don't have to repeat the wiring.
func newFullSetupService(t *testing.T, cfg *config.Config) (*handlers.SetupService, *fullDeps) {
	t.Helper()

	deps := &fullDeps{
		Snap:     &fakeSnapshotter{},
		Host:     &fakeHostnameSetter{},
		Pass:     &fakePasswordSetter{},
		Reloader: newFakeReloader(len(handlers.ReloadServicesForTest())),
	}

	svc := &handlers.SetupService{
		Cfg:            cfg,
		Log:            zerolog.Nop(),
		UCI:            newFullSetupReader(),
		Snapshotter:    deps.Snap,
		HostnameSetter: deps.Host,
		PasswordSetter: deps.Pass,
		Reloader:       deps.Reloader,
		Interfaces:     &fakeInterfaceProvider{},
	}

	return svc, deps
}

// waitForReloadGoroutine blocks until either every reloadService has
// been invoked or the timeout elapses. Tests use this to synchronize
// with the fire-and-forget reload goroutine.
func waitForReloadGoroutine(t *testing.T, r *fakeServiceReloader, timeout time.Duration) {
	t.Helper()

	want := len(handlers.ReloadServicesForTest())

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for i := 0; i < want; i++ {
		select {
		case <-r.done:
			// Got one.
		case <-deadline.C:
			t.Fatalf("reload goroutine did not finish within %s (got %d/%d)",
				timeout, i, want)
		}
	}
}

func TestApplySetup_MeshPointExtender_HappyPath(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)

	collector := &streamCollector{}

	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)
	require.NoError(t, err, "happy path should return nil")

	// Snapshotter was invoked exactly once.
	assert.Equal(t, 1, deps.Snap.snapshotCalls)
	assert.Equal(t, 0, deps.Snap.restoreCalls, "happy path must not roll back")

	// Snapshot scope covers every wizard config, including the
	// openmanetd flags file staged in phase 9.
	require.NotNil(t, deps.Snap.lastSnapshot)
	assert.ElementsMatch(t, []string{
		"wireless", "network", "dhcp", "firewall", "system", "mesh11sd", "umdns", "openmanetd", "luci",
	}, deps.Snap.lastSnapshot.configs)

	// Hostname setter saw the user's hostname.
	assert.Equal(t, []string{"openmanet-1"}, deps.Host.calls)

	// Password setter saw root + the admin password.
	calls := deps.Pass.callsCopy()
	require.Len(t, calls, 1)
	assert.Equal(t, "root", calls[0].username)
	assert.Equal(t, "supersecret", calls[0].password)

	// All 15 phases fired (PHASE_VALIDATE through PHASE_PERSIST_FLAGS)
	// plus PHASE_TERMINAL with success.
	wantPhases := []setupv1.ApplySetupResponse_Phase{
		setupv1.ApplySetupResponse_PHASE_VALIDATE,
		setupv1.ApplySetupResponse_PHASE_SNAPSHOT,
		setupv1.ApplySetupResponse_PHASE_RESET_WIRELESS,
		setupv1.ApplySetupResponse_PHASE_RESET_NETWORK,
		setupv1.ApplySetupResponse_PHASE_HOSTNAME,
		setupv1.ApplySetupResponse_PHASE_SET_TIMEZONE,
		setupv1.ApplySetupResponse_PHASE_BASE_NETWORK,
		setupv1.ApplySetupResponse_PHASE_WIRELESS_MESH,
		setupv1.ApplySetupResponse_PHASE_PER_RADIO_AP_STA,
		setupv1.ApplySetupResponse_PHASE_SCENARIO_TOPOLOGY,
		setupv1.ApplySetupResponse_PHASE_BATMAN_ADV,
		setupv1.ApplySetupResponse_PHASE_MESH11SD,
		setupv1.ApplySetupResponse_PHASE_COMMIT,
		setupv1.ApplySetupResponse_PHASE_PASSWORD,
		setupv1.ApplySetupResponse_PHASE_PERSIST_FLAGS,
	}

	for _, want := range wantPhases {
		found := false

		for _, ev := range collector.sent {
			if ev.GetPhase() == want && ev.GetStatus() == setupv1.ApplySetupResponse_STATUS_DONE {
				found = true

				break
			}
		}

		assert.Truef(t, found, "missing DONE event for phase %v", want)
	}

	// Last event is PHASE_TERMINAL DONE with success=true.
	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())
	assert.Equal(t, setupv1.ApplySetupResponse_STATUS_DONE, last.GetStatus())

	require.NotNil(t, last.GetResult())
	assert.True(t, last.GetResult().GetSuccess())
	assert.Equal(t, "https://openmanet-1.local:8081/login", last.GetResult().GetExpectedUrl())

	// Reload goroutine should have run reload on every wizard service.
	waitForReloadGoroutine(t, deps.Reloader, 2*time.Second)
	assert.ElementsMatch(t, handlers.ReloadServicesForTest(), deps.Reloader.reloadCallsCopy())
	assert.Empty(t, deps.Reloader.restartCallsCopy(), "all reloads succeeded; no restart fallback")

	// Setup-complete and auth-enable flags now true.
	assert.True(t, cfg.GetSetupComplete())
	assert.True(t, cfg.GetAuthEnable())
}

func TestApplySetup_MeshGateRouterEth_HappyPath(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)

	prof := minimalProfile()
	prof.Role = setupv1.MeshRole_MESH_ROLE_MESH_GATE
	prof.DeviceMode = &setupv1.MeshNodeProfile_MeshgateMode{
		MeshgateMode: setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER,
	}
	prof.Uplink = &setupv1.Uplink{
		Type:         setupv1.UplinkType_UPLINK_TYPE_ETHERNET,
		EthernetPort: "eth0",
	}

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), prof, collector)
	require.NoError(t, err)

	assert.Equal(t, 1, deps.Snap.snapshotCalls)
	assert.Equal(t, 0, deps.Snap.restoreCalls)

	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())
	require.NotNil(t, last.GetResult())
	assert.True(t, last.GetResult().GetSuccess())
}

// TestApplySetup_DisabledAPClearsStaleCredentials seeds a wifi-iface
// with stale ssid/key/encryption left over from a prior wizard run
// (these options survive the reset phase because they're on the
// wizard's wifi-iface whitelist), then applies a profile with that
// radio disabled. The comment on writeAPIface's disabled branch
// claims credentials are cleared; this pins that the code actually
// does it, not just marks disabled=1.
func TestApplySetup_DisabledAPClearsStaleCredentials(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, _ := newFullSetupService(t, cfg)

	reader, ok := svc.UCI.(*fakeConfigReader)
	require.True(t, ok, "fakeConfigReader expected on UCI field")

	// Seed default_radio0 with stale credentials from a prior run.
	reader.data["wireless"]["default_radio0"] = map[string][]string{
		"device":     {"radio0"},
		"mode":       {"ap"},
		"ssid":       {"stale-ssid"},
		"key":        {"stale-passphrase"},
		"encryption": {"psk2"},
	}
	reader.sectionTypes["wireless"]["default_radio0"] = "wifi-iface"

	prof := pointExtenderProfile()
	prof.Aps = []*setupv1.RadioApProfile{
		{
			RadioName: "radio0",
			Enabled:   false, // operator chose to disable
		},
	}

	require.NoError(t, runApplySetup(t, svc, prof))

	ssid, _ := reader.Get("wireless", "default_radio0", "ssid")
	assert.Empty(t, ssid, "stale ssid must be cleared when the AP is disabled")

	key, _ := reader.Get("wireless", "default_radio0", "key")
	assert.Empty(t, key, "stale key must be cleared when the AP is disabled")

	encryption, _ := reader.Get("wireless", "default_radio0", "encryption")
	assert.Empty(t, encryption, "stale encryption must be cleared when the AP is disabled")

	disabled, _ := reader.Get("wireless", "default_radio0", "disabled")
	assert.Equal(t, []string{"1"}, disabled, "disabled=1 must still be written")
}

func TestApplySetup_SnapshotFailureRollsBack(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)
	deps.Snap.snapshotErr = assert.AnError

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)

	requireConnectCode(t, err, connect.CodeInternal)

	// Snapshot fails BEFORE any mutation, so no rollback is needed.
	assert.Equal(t, 0, deps.Snap.restoreCalls)

	// Final event is the terminal failure with PHASE_SNAPSHOT.
	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())

	require.NotNil(t, last.GetResult())
	assert.False(t, last.GetResult().GetSuccess())
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_SNAPSHOT,
		last.GetResult().GetFailedPhase())
}

func TestApplySetup_HostnameSetterFailureRollsBack(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)
	deps.Host.err = assert.AnError

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)

	requireConnectCode(t, err, connect.CodeInternal)

	// Snapshot was taken; rollback was invoked exactly once.
	assert.Equal(t, 1, deps.Snap.snapshotCalls)
	assert.Equal(t, 1, deps.Snap.restoreCalls)

	// Last event reports PHASE_HOSTNAME as the failed phase.
	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())
	require.NotNil(t, last.GetResult())
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_HOSTNAME,
		last.GetResult().GetFailedPhase())

	// SetupComplete must NOT flip on a failed apply — the load-bearing
	// invariant that keeps the wizard reachable for retry. AuthEnable's
	// default depends on the firmware build, so we don't assert on it
	// here; the flag flip happens in PersistSetupAndAuth which never
	// runs when the apply fails before phase 14.
	assert.False(t, cfg.GetSetupComplete())
}

func TestApplySetup_CommitFailureRollsBack(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)

	reader, ok := svc.UCI.(*fakeConfigReader)
	require.True(t, ok)

	reader.commitError = assert.AnError

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)

	requireConnectCode(t, err, connect.CodeInternal)

	assert.Equal(t, 1, deps.Snap.snapshotCalls)
	assert.Equal(t, 1, deps.Snap.restoreCalls)

	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())
	require.NotNil(t, last.GetResult())
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_COMMIT,
		last.GetResult().GetFailedPhase())
}

func TestApplySetup_PasswordFailureRollsBackUCIButNotPassword(t *testing.T) {
	// Password failure rolls UCI back from the snapshot but leaves
	// the password as-is (the user knows what they typed; re-running
	// chpasswd from the rollback could clobber a half-set password).
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)
	deps.Pass.err = assert.AnError

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)
	requireConnectCode(t, err, connect.CodeInternal)

	// Snapshot taken, rollback invoked.
	assert.Equal(t, 1, deps.Snap.snapshotCalls)
	assert.Equal(t, 1, deps.Snap.restoreCalls)

	// Password setter was called but failed.
	require.Len(t, deps.Pass.callsCopy(), 1)

	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())
	require.NotNil(t, last.GetResult())
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_PASSWORD,
		last.GetResult().GetFailedPhase())

	// Flags are NOT flipped by the wizard on password failure. The
	// wizard's PersistSetupAndAuth call is what flips both of these,
	// and it never runs when phase 13 fails. SetupComplete defaults
	// to false; AuthEnable defaults to true for the firmware build,
	// so we just assert SetupComplete remains false (the load-bearing
	// invariant — the wizard stays reachable for retry).
	assert.False(t, cfg.GetSetupComplete())
}

func TestApplySetup_PersistFlagsFailureDoesNotRollback(t *testing.T) {
	// PersistFlags failure leaves UCI in the new state but flags
	// stay false. The user re-runs the wizard; the reset phases
	// handle leftover state. UCI rollback would actively make this
	// worse — the device is already in its target UCI state, just
	// missing the durable flag flip.
	//
	// We trigger the failure by handing the service a *config.Config
	// with setup.enabled=true but no config file path, so
	// PersistSetupAndAuth's "no config file path configured" check
	// fails.
	v := viper.New()
	v.Set("setup.enabled", true)

	cfg := config.NewWithoutWatch(v)
	deps := &fullDeps{
		Snap:     &fakeSnapshotter{},
		Host:     &fakeHostnameSetter{},
		Pass:     &fakePasswordSetter{},
		Reloader: newFakeReloader(len(handlers.ReloadServicesForTest())),
	}

	svc := &handlers.SetupService{
		Cfg:            cfg,
		Log:            zerolog.Nop(),
		UCI:            newFullSetupReader(),
		Snapshotter:    deps.Snap,
		HostnameSetter: deps.Host,
		PasswordSetter: deps.Pass,
		Reloader:       deps.Reloader,
		Interfaces:     &fakeInterfaceProvider{},
	}

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)
	requireConnectCode(t, err, connect.CodeInternal)

	// Phase 14 fail: do NOT roll back.
	assert.Equal(t, 1, deps.Snap.snapshotCalls)
	assert.Equal(t, 0, deps.Snap.restoreCalls,
		"phase 14 failure must NOT roll back UCI")

	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())
	require.NotNil(t, last.GetResult())
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_PERSIST_FLAGS,
		last.GetResult().GetFailedPhase())
}

func TestApplySetup_ReloadFailuresFallBackToRestart(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)

	// Configure the reloader to fail every Reload but succeed on
	// Restart so the fallback path is exercised.
	deps.Reloader.reloadErr = assert.AnError

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)
	require.NoError(t, err, "happy-path apply still succeeds when reload falls back")

	waitForReloadGoroutine(t, deps.Reloader, 2*time.Second)

	assert.ElementsMatch(t, handlers.ReloadServicesForTest(), deps.Reloader.reloadCallsCopy())
	assert.ElementsMatch(t, handlers.ReloadServicesForTest(), deps.Reloader.restartCallsCopy())
}

func TestApplySetup_ReloadCompleteFailureAbortsBeforeFlagFlip(t *testing.T) {
	// Both reload AND restart fail for every service. Post-bricking
	// restructure: this signals init.d itself is broken, so the wizard
	// aborts BEFORE flipping setup.complete=true. Leaving the flag
	// false means the user can re-run the wizard rather than being
	// permanently locked out — the load-bearing safety property the
	// previous "still returns success" behavior didn't have.
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)
	deps.Reloader.reloadErr = assert.AnError
	deps.Reloader.restartErr = assert.AnError

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)
	requireConnectCode(t, err, connect.CodeInternal)

	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())
	require.NotNil(t, last.GetResult())
	assert.False(t, last.GetResult().GetSuccess())
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_RELOAD_SERVICES,
		last.GetResult().GetFailedPhase())

	// setup.complete must still be false so the wizard remains
	// reachable for retry.
	assert.False(t, cfg.GetSetupComplete())
}

func TestApplySetup_NoSnapshotterIsAllowed(t *testing.T) {
	// Without a snapshotter configured the wizard still runs but
	// rollback becomes a no-op. Useful for environments that handle
	// snapshotting at a different layer. Without a PasswordSetter,
	// phase 13 fails with CodeInternal — that's expected here; the
	// test just verifies the snapshot phase emits its events.
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")

	svc := &handlers.SetupService{
		Cfg:        cfg,
		Log:        zerolog.Nop(),
		UCI:        newFullSetupReader(),
		Interfaces: &fakeInterfaceProvider{},
	}

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)

	// Without a PasswordSetter, phase 13 fails with Internal.
	requireConnectCode(t, err, connect.CodeInternal)

	// Snapshot phase emitted both events even without a snapshotter.
	startedFound := false
	doneFound := false

	for _, ev := range collector.sent {
		if ev.GetPhase() != setupv1.ApplySetupResponse_PHASE_SNAPSHOT {
			continue
		}

		switch ev.GetStatus() {
		case setupv1.ApplySetupResponse_STATUS_STARTED:
			startedFound = true
		case setupv1.ApplySetupResponse_STATUS_DONE:
			doneFound = true
		}
	}

	assert.True(t, startedFound)
	assert.True(t, doneFound)
}

func TestApplySetup_PhasesEmitInCorrectOrder(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)

	collector := &streamCollector{}
	_ = svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)

	// Filter to STARTED events and assert the order matches the
	// canonical phase sequence (14 mutation phases plus the
	// terminal event).
	startedSequence := []setupv1.ApplySetupResponse_Phase{}

	for _, ev := range collector.sent {
		if ev.GetStatus() == setupv1.ApplySetupResponse_STATUS_STARTED {
			startedSequence = append(startedSequence, ev.GetPhase())
		}
	}

	assert.Equal(t, []setupv1.ApplySetupResponse_Phase{
		setupv1.ApplySetupResponse_PHASE_VALIDATE,
		setupv1.ApplySetupResponse_PHASE_SNAPSHOT,
		setupv1.ApplySetupResponse_PHASE_RESET_WIRELESS,
		setupv1.ApplySetupResponse_PHASE_RESET_NETWORK,
		setupv1.ApplySetupResponse_PHASE_HOSTNAME,
		setupv1.ApplySetupResponse_PHASE_SET_TIMEZONE,
		setupv1.ApplySetupResponse_PHASE_BASE_NETWORK,
		setupv1.ApplySetupResponse_PHASE_WIRELESS_MESH,
		setupv1.ApplySetupResponse_PHASE_PER_RADIO_AP_STA,
		setupv1.ApplySetupResponse_PHASE_SCENARIO_TOPOLOGY,
		setupv1.ApplySetupResponse_PHASE_BATMAN_ADV,
		setupv1.ApplySetupResponse_PHASE_MESH11SD,
		setupv1.ApplySetupResponse_PHASE_COMMIT,
		setupv1.ApplySetupResponse_PHASE_PASSWORD,
		setupv1.ApplySetupResponse_PHASE_RELOAD_SERVICES,
		setupv1.ApplySetupResponse_PHASE_PERSIST_FLAGS,
	}, startedSequence)

	_ = deps // reload now runs synchronously; no goroutine wait needed
}

// TestWizardConfigsIncludesUmdns pins that umdns is captured by the
// snapshot/rollback phase. Without it, a failure between the umdns
// write (phase 6) and phase 12's commit would restore the other six
// configs but leave a partial umdns write live on disk.
func TestWizardConfigsIncludesUmdns(t *testing.T) {
	assert.Contains(t, handlers.WizardConfigsForTest(), "umdns")
}

// TestWizardConfigsIncludesOpenmanetd pins that the openmanetd flags
// file is captured by the snapshot/rollback phase: a failure after
// phase 9 must restore dhcpconfigured/batmesh1configured too.
func TestWizardConfigsIncludesOpenmanetd(t *testing.T) {
	assert.Contains(t, handlers.WizardConfigsForTest(), "openmanetd")
}

// TestWizardConfigsIncludesLuci pins that /etc/config/luci is in the
// snapshot scope: a failure after phase 9 must roll back
// luci.wizard.used and the homepage deletion.
func TestWizardConfigsIncludesLuci(t *testing.T) {
	assert.Contains(t, handlers.WizardConfigsForTest(), "luci")
}

// TestReloadServicesIncludesUmdns pins that umdns is nudged by the
// reload phase, so a freshly-registered network list takes effect
// without a reboot.
func TestReloadServicesIncludesUmdns(t *testing.T) {
	assert.Contains(t, handlers.ReloadServicesForTest(), "umdns")
}

// ── PHASE_SET_TIMEZONE ──────────────────────────────────────────────────────

// failingSystemWriteReader wraps a *fakeConfigReader and fails only
// SetType calls that write the system config's zonename/timezone
// options. Every earlier phase (reset wireless/network, hostname —
// hostname goes through HostnameSetter, not UCI.SetType, in
// newFullSetupService) succeeds against the same underlying data, so
// the injected failure is pinned precisely to PHASE_SET_TIMEZONE.
type failingSystemWriteReader struct {
	*fakeConfigReader
}

func (r *failingSystemWriteReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	if config == "system" && (option == "zonename" || option == "timezone") {
		return assert.AnError
	}

	return r.fakeConfigReader.SetType(config, section, option, typ, values...)
}

// TestApplySetup_TimezoneStageFailureRollsBack asserts a failure
// writing system.zonename/timezone rolls back UCI and reports
// PHASE_SET_TIMEZONE as the failed phase — the same invariant every
// sibling phase pins (see TestApplySetup_HostnameSetterFailureRollsBack
// and friends).
func TestApplySetup_TimezoneStageFailureRollsBack(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)

	inner, ok := svc.UCI.(*fakeConfigReader)
	require.True(t, ok, "fakeConfigReader expected on UCI field")

	svc.UCI = &failingSystemWriteReader{fakeConfigReader: inner}

	prof := minimalProfile()
	prof.Timezone = "America/Denver"

	collector := &streamCollector{}
	err := svc.ApplySetupForTest(context.Background(), prof, collector)

	requireConnectCode(t, err, connect.CodeInternal)

	// Snapshot was taken; rollback was invoked exactly once.
	assert.Equal(t, 1, deps.Snap.snapshotCalls)
	assert.Equal(t, 1, deps.Snap.restoreCalls)

	last := collector.sent[len(collector.sent)-1]
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_TERMINAL, last.GetPhase())
	require.NotNil(t, last.GetResult())
	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_SET_TIMEZONE,
		last.GetResult().GetFailedPhase())
}

// TestApplySetup_EmptyTimezoneLeavesExistingUntouched asserts an empty
// profile.timezone (the operator cleared the pre-filled select) leaves
// the device's existing system.timezone value alone and never writes
// system.zonename.
func TestApplySetup_EmptyTimezoneLeavesExistingUntouched(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, _ := newFullSetupService(t, cfg)

	reader, ok := svc.UCI.(*fakeConfigReader)
	require.True(t, ok, "fakeConfigReader expected on UCI field")

	prof := minimalProfile() // Timezone left as the zero value ("")

	require.NoError(t, runApplySetup(t, svc, prof))

	tz, ok := reader.Get("system", "@system[0]", "timezone")
	require.True(t, ok)
	assert.Equal(t, []string{"UTC"}, tz, "existing timezone must be untouched when profile.timezone is empty")

	_, ok = reader.Get("system", "@system[0]", "zonename")
	assert.False(t, ok, "zonename must not be written when profile.timezone is empty")
}

// TestApplySetup_UnknownTimezoneRejected asserts a timezone name not
// present in the embedded tzinfo table fails validation with
// CodeInvalidArgument before any mutation phase runs.
func TestApplySetup_UnknownTimezoneRejected(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, _ := newFullSetupService(t, cfg)

	prof := minimalProfile()
	prof.Timezone = "Nowhere/Fake"

	err := runApplySetup(t, svc, prof)
	requireConnectCode(t, err, connect.CodeInvalidArgument)
}

// TestApplySetup_TimezoneClockSync exercises the clock-drift threshold
// in both directions: a client clock 30s AHEAD of the device (positive
// driftSecs) and 30s BEHIND the device (negative driftSecs) both
// trigger exactly one SetTimeFn call with the client's time — pinning
// both the negated and un-negated paths of syncClock's abs(drift)
// computation. A 5s drift (below the 10s threshold) never syncs.
func TestApplySetup_TimezoneClockSync(t *testing.T) {
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		driftSecs  int
		wantCalled bool
	}{
		{name: "30s drift (client ahead of device) triggers sync", driftSecs: 30, wantCalled: true},
		{name: "30s drift (client behind device) also triggers sync", driftSecs: -30, wantCalled: true},
		{name: "5s drift does not trigger sync", driftSecs: 5, wantCalled: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
			svc, _ := newFullSetupService(t, cfg)
			svc.SetNowFnForTest(func() time.Time { return fixedNow })

			calls := 0

			var calledWith time.Time

			svc.SetTimeFn = func(tt time.Time) error {
				calls++
				calledWith = tt

				return nil
			}

			clientTime := fixedNow.Add(time.Duration(tc.driftSecs) * time.Second)

			prof := minimalProfile()
			prof.Timezone = "America/Denver"
			prof.ClientTime = timestamppb.New(clientTime)

			require.NoError(t, runApplySetup(t, svc, prof))

			if tc.wantCalled {
				assert.Equal(t, 1, calls)
				assert.True(t, calledWith.Equal(clientTime), "SetTimeFn must be called with the client's time")

				return
			}

			assert.Equal(t, 0, calls)
		})
	}
}

// TestApplySetup_TimezoneClockSyncSetTimeFnErrorStillSucceeds asserts
// a failing SetTimeFn is best-effort: the phase (and the whole apply)
// still succeeds.
func TestApplySetup_TimezoneClockSyncSetTimeFnErrorStillSucceeds(t *testing.T) {
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, _ := newFullSetupService(t, cfg)
	svc.SetNowFnForTest(func() time.Time { return fixedNow })
	svc.SetTimeFn = func(time.Time) error { return assert.AnError }

	prof := minimalProfile()
	prof.Timezone = "America/Denver"
	prof.ClientTime = timestamppb.New(fixedNow.Add(30 * time.Second))

	require.NoError(t, runApplySetup(t, svc, prof), "SetTimeFn failure must not fail the phase")
}
