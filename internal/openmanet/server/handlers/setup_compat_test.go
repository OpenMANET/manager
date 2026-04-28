package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/digineo/go-uci/v2"
	setupv1 "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1"
	wificonfigv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setup_compat_test.go enforces the structural invariants the wizard
// MUST produce for the device to boot in a usable state. These are not
// byte-equivalence tests against captured fixtures (the captures came
// from heterogeneous SKUs and include operator-set fields outside the
// wizard's scope) — they are direct assertions on the staged UCI tree
// after ApplySetup runs.
//
// The bricking incident on 2026-04-28 was caused by the handler
// completing every phase event but never writing several load-bearing
// UCI sections (network.ahwlan interface, br-ahwlan bridge, dhcp pool,
// wifi-iface network bindings, role-aware gw_mode). These tests pin
// down each of those invariants so the same regression cannot recur
// silently.

// uciTree exposes the contents of a fakeConfigReader after ApplySetup
// in a shape convenient for cross-config invariants. The fields are
// populated via runScenarioApply.
type uciTree struct {
	reader *fakeConfigReader
}

func (t *uciTree) hasSection(config, section string) bool {
	if t.reader.sectionTypes[config] == nil {
		return false
	}

	_, ok := t.reader.sectionTypes[config][section]

	return ok
}

func (t *uciTree) sectionsOfType(config, secType string) []string {
	out := []string{}
	if t.reader.sectionTypes[config] == nil {
		return out
	}

	for s, st := range t.reader.sectionTypes[config] {
		if st == secType {
			out = append(out, s)
		}
	}

	return out
}

func (t *uciTree) get(config, section, option string) []string {
	v, _ := t.reader.Get(config, section, option)

	return v
}

func (t *uciTree) getOne(config, section, option string) string {
	v := t.get(config, section, option)
	if len(v) == 0 {
		return ""
	}

	return v[0]
}

// findBridgeDevice returns the section name of the `config device`
// whose `name` field equals bridgeName, or "" if no such bridge exists.
func (t *uciTree) findBridgeDevice(bridgeName string) string {
	for _, s := range t.sectionsOfType("network", "device") {
		if t.getOne("network", s, "name") == bridgeName {
			return s
		}
	}

	return ""
}

// findFirewallZoneByName returns the section name of the `config zone`
// whose `name` field equals zoneName.
func (t *uciTree) findFirewallZoneByName(zoneName string) string {
	for _, s := range t.sectionsOfType("firewall", "zone") {
		if t.getOne("firewall", s, "name") == zoneName {
			return s
		}
	}

	return ""
}

// findFirewallForwarding returns the section name of the first
// `config forwarding` matching (src, dest), or "" if none.
func (t *uciTree) findFirewallForwarding(src, dest string) string {
	for _, s := range t.sectionsOfType("firewall", "forwarding") {
		if t.getOne("firewall", s, "src") == src &&
			t.getOne("firewall", s, "dest") == dest {
			return s
		}
	}

	return ""
}

// findDhcpPool returns the section name of the first `config dhcp`
// whose `interface` equals iface and which is not marked `ignore=1`.
func (t *uciTree) findDhcpPool(iface string) string {
	for _, s := range t.sectionsOfType("dhcp", "dhcp") {
		if t.getOne("dhcp", s, "interface") != iface {
			continue
		}

		if t.getOne("dhcp", s, "ignore") == "1" {
			continue
		}

		return s
	}

	return ""
}

// runScenarioApply runs ApplySetup with the supplied profile against
// a fresh full-setup reader and returns the staged UCI tree. The test
// asserts NO error from ApplySetup — the wizard must complete cleanly.
func runScenarioApply(t *testing.T, profile *setupv1.MeshNodeProfile) *uciTree {
	t.Helper()

	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, _ := newFullSetupService(t, cfg)

	collector := &streamCollector{}
	require.NoError(t, svc.ApplySetupForTest(context.Background(), profile, collector),
		"ApplySetup must complete cleanly on the happy path")

	reader, ok := svc.UCI.(*fakeConfigReader)
	require.True(t, ok, "fakeConfigReader expected on UCI field")

	return &uciTree{reader: reader}
}

// gateRouterEthProfile returns a fully-valid mesh-gate router-on-eth
// profile, the most common gateway scenario.
func gateRouterEthProfile() *setupv1.MeshNodeProfile {
	prof := minimalProfile()
	prof.Hostname = "test-gate"
	prof.Role = setupv1.MeshRole_MESH_ROLE_MESH_GATE
	prof.DeviceMode = &setupv1.MeshNodeProfile_MeshgateMode{
		MeshgateMode: setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER,
	}
	prof.Uplink = &setupv1.Uplink{
		Type:         setupv1.UplinkType_UPLINK_TYPE_ETHERNET,
		EthernetPort: "eth0",
	}
	prof.Aps = []*setupv1.RadioApProfile{
		{
			RadioName:  "radio0",
			Enabled:    true,
			Ssid:       "test-ap-2g",
			Passphrase: "appassword",
			Encryption: wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK2,
		},
	}

	return prof
}

// pointExtenderProfile returns a fully-valid mesh-point-extender
// profile, the most common non-gateway scenario.
func pointExtenderProfile() *setupv1.MeshNodeProfile {
	prof := minimalProfile()
	prof.Hostname = "test-point"
	prof.Aps = []*setupv1.RadioApProfile{
		{
			RadioName:  "radio0",
			Enabled:    true,
			Ssid:       "test-ap-2g",
			Passphrase: "appassword",
			Encryption: wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_PSK2,
		},
	}

	return prof
}

// ── Universal invariants (every scenario) ─────────────────────────────────────

// TestCompat_BatmanDeviceWritten asserts bat0 is configured with all
// the batman-adv knobs and a role-appropriate gw_mode.
func TestCompat_BatmanDeviceWritten_Gate(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())
	assertBatmanDevice(t, tr, "server")
}

func TestCompat_BatmanDeviceWritten_Point(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())
	assertBatmanDevice(t, tr, "client")
}

func assertBatmanDevice(t *testing.T, tr *uciTree, wantGwMode string) {
	t.Helper()
	require.True(t, tr.hasSection("network", "bat0"),
		"network.bat0 interface must exist after wizard run")

	assert.Equal(t, "batadv", tr.getOne("network", "bat0", "proto"),
		"bat0.proto must be batadv")
	assert.Equal(t, "BATMAN_V", tr.getOne("network", "bat0", "routing_algo"),
		"bat0.routing_algo must be BATMAN_V")
	assert.Equal(t, wantGwMode, tr.getOne("network", "bat0", "gw_mode"),
		"bat0.gw_mode must match role (server for gates, client for points)")
	assert.Equal(t, "1", tr.getOne("network", "bat0", "bridge_loop_avoidance"))
	assert.Equal(t, "30", tr.getOne("network", "bat0", "hop_penalty"))
	assert.Equal(t, "1", tr.getOne("network", "bat0", "fragmentation"))
	assert.Equal(t, "1000", tr.getOne("network", "bat0", "orig_interval"))
}

// TestCompat_BatmeshHardifsWritten asserts batmesh0 + batmesh1 exist
// with proto=batadv_hardif master=bat0.
func TestCompat_BatmeshHardifsWritten(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	for _, iface := range []string{"batmesh0", "batmesh1"} {
		require.Truef(t, tr.hasSection("network", iface),
			"network.%s must exist", iface)
		assert.Equal(t, "batadv_hardif",
			tr.getOne("network", iface, "proto"),
			"%s.proto must be batadv_hardif", iface)
		assert.Equal(t, "bat0",
			tr.getOne("network", iface, "master"),
			"%s.master must be bat0", iface)
	}
}

// TestCompat_AhwlanInterfaceWritten asserts the network.ahwlan
// interface section exists with the LuCI-equivalent options. THIS IS
// THE LOAD-BEARING SECTION — without it, the device has no L3 on the
// management network and is unreachable.
func TestCompat_AhwlanInterfaceWritten_Gate(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())
	assertAhwlanInterface(t, tr)
}

func TestCompat_AhwlanInterfaceWritten_Point(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())
	assertAhwlanInterface(t, tr)
}

func assertAhwlanInterface(t *testing.T, tr *uciTree) {
	t.Helper()
	require.True(t, tr.hasSection("network", "ahwlan"),
		"network.ahwlan interface MUST be created — without it the device has no management IP")

	assert.Equal(t, "static", tr.getOne("network", "ahwlan", "proto"),
		"ahwlan.proto must be static")
	assert.Equal(t, "255.255.0.0", tr.getOne("network", "ahwlan", "netmask"),
		"ahwlan.netmask must be 255.255.0.0")
	assert.Equal(t, "64", tr.getOne("network", "ahwlan", "ip6assign"),
		"ahwlan.ip6assign must be 64")
	assert.Equal(t, "eui64", tr.getOne("network", "ahwlan", "ip6ifaceid"),
		"ahwlan.ip6ifaceid must be eui64 (required for batman/alfred over IPv6)")
	assert.Equal(t, "br-ahwlan", tr.getOne("network", "ahwlan", "device"),
		"ahwlan.device must point at the br-ahwlan bridge")

	ip6class := tr.get("network", "ahwlan", "ip6class")
	assert.Contains(t, ip6class, "local",
		"ahwlan.ip6class must include 'local'")
}

// TestCompat_AhwlanIPaddrSet_Point asserts mesh-point devices get a
// random ipaddr in the mesh subnet. (Mesh-gate scenario does NOT set
// ipaddr — openmanetd handles that at runtime.)
func TestCompat_AhwlanIPaddrSet_Point(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())

	ip := tr.getOne("network", "ahwlan", "ipaddr")
	require.NotEmpty(t, ip, "mesh-point ahwlan.ipaddr must be set to a random in-subnet address")
	assert.True(t, strings.HasPrefix(ip, "10.41."),
		"ahwlan.ipaddr must be inside the mesh subnet (got %q)", ip)
	assert.NotEqual(t, "10.41.254.1", ip,
		"ahwlan.ipaddr must avoid the factory IP")
}

// TestCompat_BrAhwlanBridgeWritten asserts a `config device` of
// type=bridge name=br-ahwlan exists with bat0 in its ports list and
// an F2:-prefixed MAC. THIS IS THE LOAD-BEARING SECTION — without it,
// ethernet ports are orphaned and ahwlan has no L2 path to anything.
func TestCompat_BrAhwlanBridgeWritten_Gate(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())
	assertBrAhwlanBridge(t, tr)
}

func TestCompat_BrAhwlanBridgeWritten_Point(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())
	assertBrAhwlanBridge(t, tr)
}

func assertBrAhwlanBridge(t *testing.T, tr *uciTree) {
	t.Helper()

	bridge := tr.findBridgeDevice("br-ahwlan")
	require.NotEmpty(t, bridge,
		"a config device with name=br-ahwlan must exist — without it, ethernet ports are orphaned")

	assert.Equal(t, "bridge", tr.getOne("network", bridge, "type"))

	macaddr := tr.getOne("network", bridge, "macaddr")
	assert.True(t, strings.HasPrefix(strings.ToUpper(macaddr), "F2:"),
		"br-ahwlan.macaddr must use the F2 OUI prefix (got %q)", macaddr)

	ports := tr.get("network", bridge, "ports")
	assert.Contains(t, ports, "bat0",
		"br-ahwlan.ports must include bat0 — without it, batman traffic doesn't bridge to ethernet/wifi")
}

// TestCompat_MeshIfaceBoundToBatmesh0 asserts the morse mesh
// wifi-iface has `network=batmesh0`. THIS IS THE LOAD-BEARING OPTION —
// without it, the wifi driver cannot bind the mesh radio to the
// batman-adv hardif and the mesh radio never comes up.
func TestCompat_MeshIfaceBoundToBatmesh0(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	// The mesh iface name follows the `default_<radio>` convention.
	const meshIface = "default_radio1"
	require.True(t, tr.hasSection("wireless", meshIface),
		"mesh wifi-iface %s must exist", meshIface)

	assert.Equal(t, "mesh", tr.getOne("wireless", meshIface, "mode"))
	assert.Equal(t, "batmesh0", tr.getOne("wireless", meshIface, "network"),
		"mesh wifi-iface MUST have network=batmesh0 — without it, the mesh radio doesn't bind to batman")
}

// TestCompat_APBoundToAhwlan asserts AP wifi-ifaces get
// `network=ahwlan` (the gate scenario binding). Mesh-point-extender
// has its own binding tested separately.
func TestCompat_APBoundToAhwlan_Gate(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	// radio0 is the AP in our profile. The iface name is
	// default_radio0.
	const apIface = "default_radio0"
	require.True(t, tr.hasSection("wireless", apIface),
		"AP wifi-iface %s must exist", apIface)

	assert.Equal(t, "ap", tr.getOne("wireless", apIface, "mode"))
	assert.Equal(t, "ahwlan", tr.getOne("wireless", apIface, "network"),
		"AP wifi-iface MUST have network=ahwlan in gate scenarios — without it, the AP isn't bridged to the management network")
}

// TestCompat_DisabledAPHasDisabledOption asserts disabled APs in the
// profile produce a wifi-iface with `disabled=1` rather than being
// silently skipped (which leaves any pre-existing iface in place).
func TestCompat_DisabledAPHasDisabledOption(t *testing.T) {
	prof := pointExtenderProfile()
	prof.Aps = []*setupv1.RadioApProfile{
		{
			RadioName:  "radio0",
			Enabled:    false, // operator chose to disable
			Encryption: wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_NONE,
		},
	}

	tr := runScenarioApply(t, prof)

	const apIface = "default_radio0"
	require.True(t, tr.hasSection("wireless", apIface),
		"AP iface section must exist (created by reset phase)")

	assert.Equal(t, "1", tr.getOne("wireless", apIface, "disabled"),
		"disabled APs must have disabled=1 written; skipping with `continue` leaves the iface enabled")
}

// TestCompat_AhwlanDhcpPoolCreated asserts a DHCP pool exists for
// ahwlan with the LuCI-shape options. Without this, clients on the
// management network can't get an IP.
func TestCompat_AhwlanDhcpPoolCreated(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	pool := tr.findDhcpPool("ahwlan")
	require.NotEmpty(t, pool,
		"a dhcp pool for ahwlan must exist — without it, clients can't get IPs")

	assert.Equal(t, "ahwlan", tr.getOne("dhcp", pool, "interface"))
	assert.Equal(t, "16", tr.getOne("dhcp", pool, "limit"))
	assert.NotEmpty(t, tr.getOne("dhcp", pool, "start"),
		"dhcp pool must have a start offset")
	assert.NotEmpty(t, tr.getOne("dhcp", pool, "leasetime"),
		"dhcp pool must have a leasetime")
	assert.Equal(t, "1", tr.getOne("dhcp", pool, "force"),
		"dhcp pool must have force=1 so dnsmasq serves even when an upstream DHCP server is on the LAN")
}

// TestCompat_DnsmasqWhitelisted asserts the per-network dnsmasq
// instance the wizard creates for ahwlan has the standard wizard
// options. The factory anonymous dnsmasq is left as a fallback (with
// non-whitelist options pruned by the reset phase).
func TestCompat_DnsmasqWhitelisted(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	// A per-network dnsmasq instance bound to ahwlan must exist; the
	// scenario phase creates it via SetupDnsmasqInstance with the
	// wizard's 11 standard options.
	const dnsmasq = "ahwlan_dns"
	require.True(t, tr.hasSection("dhcp", dnsmasq),
		"per-network ahwlan_dns dnsmasq instance must exist after the wizard runs")

	assert.Equal(t, "1000", tr.getOne("dhcp", dnsmasq, "cachesize"))
	assert.Equal(t, "1", tr.getOne("dhcp", dnsmasq, "localservice"))
	assert.Equal(t, "1", tr.getOne("dhcp", dnsmasq, "authoritative"))
	assert.Equal(t, "/ahwlan/", tr.getOne("dhcp", dnsmasq, "local"))
	assert.Equal(t, "ahwlan", tr.getOne("dhcp", dnsmasq, "domain"))
}

// TestCompat_AhwlanFirewallZone asserts the firewall zone for ahwlan
// exists with mtu_fix=1 and ACCEPT policy.
func TestCompat_AhwlanFirewallZone_Gate(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())
	assertAhwlanFirewallZone(t, tr)
}

func TestCompat_AhwlanFirewallZone_Point(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())
	assertAhwlanFirewallZone(t, tr)
}

func assertAhwlanFirewallZone(t *testing.T, tr *uciTree) {
	t.Helper()

	zone := tr.findFirewallZoneByName("ahwlan")
	require.NotEmpty(t, zone, "firewall.ahwlan zone must exist")

	assert.Equal(t, "ACCEPT", tr.getOne("firewall", zone, "input"))
	assert.Equal(t, "ACCEPT", tr.getOne("firewall", zone, "output"))
	assert.Equal(t, "ACCEPT", tr.getOne("firewall", zone, "forward"))
	assert.Equal(t, "1", tr.getOne("firewall", zone, "mtu_fix"),
		"local zones (ahwlan) must have mtu_fix=1 so VPN-style MSS clamping works correctly")

	networks := tr.get("firewall", zone, "network")
	assert.Contains(t, networks, "ahwlan",
		"ahwlan zone must list the ahwlan network")
}

// TestCompat_MmrouterForwardingForGate asserts the mmrouter forwarding
// rule (ahwlan→lan) exists for gate scenarios.
func TestCompat_MmrouterForwardingForGate(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	fwd := tr.findFirewallForwarding("ahwlan", "lan")
	require.NotEmpty(t, fwd,
		"forwarding ahwlan→lan must exist on a router-mode mesh gate (the mmrouter forward)")
}

// TestCompat_MmextenderForwardingForExtender asserts the mmextender
// forwarding rule (lan→ahwlan) exists for mesh-point-extender.
func TestCompat_MmextenderForwardingForExtender(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())

	fwd := tr.findFirewallForwarding("lan", "ahwlan")
	require.NotEmpty(t, fwd,
		"forwarding lan→ahwlan must exist on a mesh-point-extender (the mmextender forward)")
}

// TestCompat_LanProtoOnGateWithEthUplink asserts network.lan.proto is
// flipped to dhcp on a router-with-ethernet-uplink gate.
func TestCompat_LanProtoOnGateWithEthUplink(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	assert.Equal(t, "dhcp", tr.getOne("network", "lan", "proto"),
		"router-with-ethernet uplink: network.lan.proto must be dhcp")
}

// TestCompat_LanDNSAlwaysSet asserts network.lan.dns=1.1.1.1 is set
// unconditionally (LuCI does this in setupBatmanInterfaceOnDevice).
func TestCompat_LanDNSAlwaysSet(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	assert.Equal(t, "1.1.1.1", tr.getOne("network", "lan", "dns"),
		"network.lan.dns must be set to 1.1.1.1 (mirrors LuCI's setupBatmanInterfaceOnDevice)")
}

// TestCompat_Mesh11sdGateAnnouncements asserts mesh11sd reflects the
// role choice (1 for gates, 0 for points).
func TestCompat_Mesh11sdGateAnnouncements_Gate(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())
	assert.Equal(t, "1",
		tr.getOne("mesh11sd", "mesh_params", "mesh_gate_announcements"),
		"mesh gate must announce itself")
}

func TestCompat_Mesh11sdGateAnnouncements_Point(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())
	assert.Equal(t, "0",
		tr.getOne("mesh11sd", "mesh_params", "mesh_gate_announcements"),
		"mesh point must NOT announce itself as a gate")
}

// TestCompat_Mesh11sdFwdingDisabled asserts mesh_fwding=0 (batman-adv
// owns forwarding when batman is in play, which it always is here).
func TestCompat_Mesh11sdFwdingDisabled(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())
	assert.Equal(t, "0",
		tr.getOne("mesh11sd", "mesh_params", "mesh_fwding"),
		"mesh11sd.mesh_fwding must be 0 — batman-adv handles forwarding")
}

// TestCompat_HostnameWritten asserts the wizard invokes the
// hostname setter with the profile's hostname. The fake setter records
// calls without writing to UCI, so we assert against its call log
// rather than re-reading the UCI tree.
func TestCompat_HostnameWritten(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, deps := newFullSetupService(t, cfg)

	prof := gateRouterEthProfile()

	require.NoError(t, svc.ApplySetupForTest(context.Background(), prof, &streamCollector{}))

	assert.Equal(t, []string{"test-gate"}, deps.Host.calls,
		"the hostname setter must be invoked once with the profile's hostname")
}

// TestCompat_DefaultWanFirewallRulesPresent asserts the 13 wizard-
// installed firewall rules exist.
func TestCompat_DefaultWanFirewallRulesPresent(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	rules := tr.sectionsOfType("firewall", "rule")
	assert.GreaterOrEqual(t, len(rules), 13,
		"expected at least 13 default firewall rules added by the wizard")

	// Spot-check a few key rules by their option signatures.
	wantNames := map[string]bool{
		"Allow-DHCP-Renew":              false,
		"Allow Batman Mesh TCP 4242":    false,
		"Block-DHCP-Request-Out-ahwlan": false,
		"Block-DHCP-Response-In-ahwlan": false,
	}

	for _, r := range rules {
		name := tr.getOne("firewall", r, "name")
		if _, ok := wantNames[name]; ok {
			wantNames[name] = true
		}
	}

	for name, found := range wantNames {
		assert.Truef(t, found, "expected wizard to write firewall rule %q", name)
	}
}

// ── LuCI Morse parity invariants (post-2026-04-28 review) ────────────────────
//
// These tests pin down the five behavioral gaps identified in the
// detailed Morse-vs-Go comparison. They lock in the LuCI semantics
// the new wizard must reproduce so the device boots into a
// functionally equivalent end-state.

// pointNoneProfile returns a fully-valid mesh-point-none profile
// (headless mesh node — joins mesh, no extender role).
func pointNoneProfile() *setupv1.MeshNodeProfile {
	prof := minimalProfile()
	prof.Hostname = "test-point-none"
	prof.DeviceMode = &setupv1.MeshNodeProfile_MeshpointMode{
		MeshpointMode: setupv1.MeshPointMode_MESH_POINT_MODE_NONE,
	}
	prof.Aps = []*setupv1.RadioApProfile{}

	return prof
}

// gateRouterFirewallEthProfile returns a fully-valid mesh-gate
// router_firewall + ethernet uplink profile (the wan-zone scenario).
func gateRouterFirewallEthProfile() *setupv1.MeshNodeProfile {
	prof := minimalProfile()
	prof.Hostname = "test-gate-fw"
	prof.Role = setupv1.MeshRole_MESH_ROLE_MESH_GATE
	prof.DeviceMode = &setupv1.MeshNodeProfile_MeshgateMode{
		MeshgateMode: setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER_FIREWALL,
	}
	prof.Uplink = &setupv1.Uplink{
		Type:         setupv1.UplinkType_UPLINK_TYPE_ETHERNET,
		EthernetPort: "eth0",
	}

	return prof
}

// Gap 1: scenarioMeshPointNone must put ahwlan into DHCP-CLIENT mode
// and run the DHCP SERVER on lan, not on ahwlan. This is the LuCI 4b
// semantic — a "headless" mesh point pulls its address from a peer
// mesh-gate over the mesh and serves DHCP only on its LAN side.
func TestCompat_MeshPointNone_AhwlanIsDhcpClient(t *testing.T) {
	tr := runScenarioApply(t, pointNoneProfile())

	assert.Equal(t, "dhcp", tr.getOne("network", "ahwlan", "proto"),
		"mesh-point-none ahwlan must be a DHCP client (the peer mesh-gate provides the address)")
	assert.Empty(t, tr.get("network", "ahwlan", "ipaddr"),
		"mesh-point-none ahwlan must NOT have a static ipaddr")
}

// Gap 1 (cont.): mesh-point-none serves DHCP on lan, not on ahwlan,
// with the no-router/no-DNS option codes set.
func TestCompat_MeshPointNone_DhcpServerOnLan(t *testing.T) {
	tr := runScenarioApply(t, pointNoneProfile())

	lanPool := tr.findDhcpPool("lan")
	require.NotEmpty(t, lanPool,
		"mesh-point-none must serve DHCP on lan (the device's downstream LAN side)")

	dhcpOption := tr.get("dhcp", lanPool, "dhcp_option")
	assert.Contains(t, dhcpOption, "3",
		"lan DHCP must suppress the default-route option (dhcp_option=3)")
	assert.Contains(t, dhcpOption, "6",
		"lan DHCP must suppress the DNS-server option (dhcp_option=6)")

	ahwlanPool := tr.findDhcpPool("ahwlan")
	assert.Empty(t, ahwlanPool,
		"mesh-point-none must NOT serve DHCP on ahwlan (it's a DHCP client there)")
}

// Gap 2: router_firewall scenario must add wan6 to the wan firewall
// zone's network list. Without this, IPv6 packets on wan6 hit the
// implicit-REJECT default zone and get dropped.
func TestCompat_RouterFirewallEth_Wan6InWanZone(t *testing.T) {
	tr := runScenarioApply(t, gateRouterFirewallEthProfile())

	wanZone := tr.findFirewallZoneByName("wan")
	require.NotEmpty(t, wanZone, "wan firewall zone must exist")

	networks := tr.get("firewall", wanZone, "network")
	assert.Contains(t, networks, "wan",
		"wan zone must include the wan network")
	assert.Contains(t, networks, "wan6",
		"wan zone must include wan6 — without it IPv6 traffic is dropped by the default-zone REJECT")
}

// Gap 2 (cont.): wan6 interface must be created with proto=dhcpv6 in
// the router_firewall scenario.
func TestCompat_RouterFirewallEth_Wan6Created(t *testing.T) {
	tr := runScenarioApply(t, gateRouterFirewallEthProfile())

	require.True(t, tr.hasSection("network", "wan6"),
		"router_firewall scenario must create network.wan6")
	assert.Equal(t, "dhcpv6", tr.getOne("network", "wan6", "proto"),
		"wan6.proto must be dhcpv6")
}

// Gap 3: every wizard run must emit the LuCI mesh-AP overlay section
// (`meshap_<mesh-radio>`) so operators can later toggle it on from the
// settings UI without having to create the section by hand. The
// section is always written disabled with default ssid/key.
func TestCompat_MeshAPOverlayEmitted(t *testing.T) {
	// Mesh radio in the test fixture is `radio1`. The overlay iface
	// must follow the LuCI `meshap_<radio>` naming.
	tr := runScenarioApply(t, gateRouterEthProfile())

	const overlayIface = "meshap_radio1"
	require.True(t, tr.hasSection("wireless", overlayIface),
		"mesh-AP overlay section %s must exist (LuCI parity, plan §Per-scenario topology)",
		overlayIface)

	assert.Equal(t, "ap", tr.getOne("wireless", overlayIface, "mode"))
	assert.Equal(t, "sae", tr.getOne("wireless", overlayIface, "encryption"))
	assert.Equal(t, "1", tr.getOne("wireless", overlayIface, "disabled"),
		"the mesh-AP overlay must always be written disabled — operators opt-in from settings")
	assert.Equal(t, "ahwlan", tr.getOne("wireless", overlayIface, "network"))
	assert.NotEmpty(t, tr.getOne("wireless", overlayIface, "ssid"),
		"mesh-AP overlay must have a default SSID set so it's usable when toggled on")
	assert.NotEmpty(t, tr.getOne("wireless", overlayIface, "key"),
		"mesh-AP overlay must have a default key set")
}

// Gap 4: scoped dnsmasq sections from the factory image must be
// removed in the reset phase. LuCI removes any dnsmasq with
// `interface` set or non-loopback `notinterface`. The wizard then
// creates per-network instances in the scenario phase.
func TestCompat_ScopedDnsmasqRemovedInReset(t *testing.T) {
	cfg := setupBLOSTestConfig(t, "setup:\n  enabled: true\n")
	svc, _ := newFullSetupService(t, cfg)

	reader, ok := svc.UCI.(*fakeConfigReader)
	require.True(t, ok)

	// Inject a scoped dnsmasq mimicking what a factory image with a
	// per-LAN dnsmasq might ship.
	require.NoError(t, reader.AddSection("dhcp", "scoped_lan_dns", "dnsmasq"))
	require.NoError(t, reader.SetType("dhcp", "scoped_lan_dns", "interface",
		uci.TypeOption, "lan"))

	require.NoError(t, svc.ApplySetupForTest(context.Background(),
		gateRouterEthProfile(), &streamCollector{}))

	// The scoped dnsmasq must be gone after the wizard runs — the
	// reset phase deletes scoped sections so the per-network
	// ahwlan_dns can take over without a competing global dnsmasq.
	assert.False(t,
		(&uciTree{reader: reader}).hasSection("dhcp", "scoped_lan_dns"),
		"reset phase must remove scoped dnsmasq sections")
}

// Gap 5: mesh-point scenarios must write `network.ahwlan.dns=1.1.1.1`.
// LuCI's setupNetworkWithDnsmasq writes this in the isMeshPoint
// branch alongside the random ipaddr — without it the mesh point
// has no DNS resolver on its mesh address.
func TestCompat_MeshPointAhwlanHasDNS(t *testing.T) {
	tr := runScenarioApply(t, pointExtenderProfile())

	assert.Equal(t, "1.1.1.1", tr.getOne("network", "ahwlan", "dns"),
		"mesh-point ahwlan must have dns=1.1.1.1 (LuCI parity, captured fixture)")
}

// Gap 5 (negative): mesh-gate scenarios must NOT write
// `network.ahwlan.dns` because the gateway's upstream provides DNS.
// LuCI's setupNetworkWithDnsmasq with isMeshPoint=false skips this
// option.
func TestCompat_MeshGateAhwlanNoDNS(t *testing.T) {
	tr := runScenarioApply(t, gateRouterEthProfile())

	assert.Empty(t, tr.get("network", "ahwlan", "dns"),
		"mesh-gate ahwlan must NOT have dns set — the upstream router provides DNS")
}

// TestCompat_RouterFirewallEth_LanProtoUntouched confirms the wan-
// upstream scenario does NOT flip lan.proto. The factory's static lan
// is preserved; only wan/wan6 carry the upstream DHCP work.
func TestCompat_RouterFirewallEth_LanProtoUntouched(t *testing.T) {
	tr := runScenarioApply(t, gateRouterFirewallEthProfile())

	// The factory-fixture lan starts at proto=static; the firewall
	// scenario doesn't promote it to dhcp.
	assert.NotEqual(t, "dhcp", tr.getOne("network", "lan", "proto"),
		"router_firewall scenario must NOT change lan.proto to dhcp (the upstream is wan)")
}
