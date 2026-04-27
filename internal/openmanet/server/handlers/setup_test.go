package handlers_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	setupv1 "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1"
	wificonfigv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
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
	cfg := setupBLOSTestConfig(t, "")
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

func TestGetSetupStatus_AlreadyConfigured_MeshGateAnnouncementsSet(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")

	reader := newSetupReader()
	if reader.data["mesh11sd"] == nil {
		reader.data["mesh11sd"] = map[string]map[string][]string{}
	}
	reader.data["mesh11sd"]["mesh_params"] = map[string][]string{
		"mesh_gate_announcements": {"1"},
	}

	svc := newSetupService(t, cfg, reader, &fakeInterfaceProvider{})

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetAlreadyConfigured())
}

func TestGetSetupStatus_AlreadyConfigured_AhwlanProtoSet(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "")

	reader := newSetupReader()
	if reader.data["network"] == nil {
		reader.data["network"] = map[string]map[string][]string{}
	}
	reader.data["network"]["ahwlan"] = map[string][]string{
		"proto": {"static"},
	}

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
			{Name: "wlan0"}, // filtered
			{Name: "br-lan"}, // filtered
			{Name: "lo"},     // filtered
			{Name: "tailscale0"}, // filtered
			{Name: "bat0"},   // filtered
		},
	}

	svc := newSetupService(t, cfg, newSetupReader(), ifaces)

	resp, err := svc.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.Equal(t, []string{"eth0", "eth1"}, resp.GetEthernetPorts())
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
	// Until the rest of the phases land, a valid profile reaches the
	// post-validation phase placeholder, which returns Unimplemented.
	// The test asserts the validate phase emitted STARTED + DONE
	// events on the stream.
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	collector := &streamCollector{}
	svc := newSetupService(t, cfg, newSetupReader(), &fakeInterfaceProvider{})

	err := svc.ApplySetupForTest(context.Background(), minimalProfile(), collector)
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnimplemented, connectErr.Code(),
		"valid profiles fall through to the unimplemented-phase guard")

	require.Len(t, collector.sent, 2, "expected STARTED and DONE events")

	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_VALIDATE, collector.sent[0].GetPhase())
	assert.Equal(t, setupv1.ApplySetupResponse_STATUS_STARTED, collector.sent[0].GetStatus())

	assert.Equal(t, setupv1.ApplySetupResponse_PHASE_VALIDATE, collector.sent[1].GetPhase())
	assert.Equal(t, setupv1.ApplySetupResponse_STATUS_DONE, collector.sent[1].GetStatus())
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
