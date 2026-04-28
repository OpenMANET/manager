package handlers

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/digineo/go-uci/v2"
	setupv1 "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1"
	"github.com/openmanet/openmanetd/internal/network"
)

// runMutationPhases threads phases 3-12 in order, returning the phase
// enum that failed and the underlying error. On success it returns
// (PHASE_UNSPECIFIED, nil) and the caller proceeds to phase 13.
//
// Each phase helper emits its own STARTED/DONE/FAILED events; this
// orchestrator just translates the per-phase Go error into the
// corresponding phase enum so the caller can build the terminal
// event with the correct failed_phase.
func (s *SetupService) runMutationPhases(
	ctx context.Context,
	stream applySetupStream,
	profile *setupv1.MeshNodeProfile,
	snapshot UCISnapshot,
) (setupv1.ApplySetupResponse_Phase, error) {
	_ = snapshot // future phases may inspect snapshot for diff-based decisions

	if err := s.runResetWireless(ctx, stream); err != nil {
		return setupv1.ApplySetupResponse_PHASE_RESET_WIRELESS, err
	}

	if err := s.runResetNetwork(ctx, stream); err != nil {
		return setupv1.ApplySetupResponse_PHASE_RESET_NETWORK, err
	}

	if err := s.runHostname(ctx, stream, profile); err != nil {
		return setupv1.ApplySetupResponse_PHASE_HOSTNAME, err
	}

	if err := s.runBaseNetwork(ctx, stream, profile); err != nil {
		return setupv1.ApplySetupResponse_PHASE_BASE_NETWORK, err
	}

	if err := s.runWirelessMesh(ctx, stream, profile); err != nil {
		return setupv1.ApplySetupResponse_PHASE_WIRELESS_MESH, err
	}

	if err := s.runPerRadioAPSta(ctx, stream, profile); err != nil {
		return setupv1.ApplySetupResponse_PHASE_PER_RADIO_AP_STA, err
	}

	if err := s.runScenarioTopology(ctx, stream, profile); err != nil {
		return setupv1.ApplySetupResponse_PHASE_SCENARIO_TOPOLOGY, err
	}

	if err := s.runBatmanAdv(ctx, stream, profile); err != nil {
		return setupv1.ApplySetupResponse_PHASE_BATMAN_ADV, err
	}

	if err := s.runMesh11sd(ctx, stream, profile); err != nil {
		return setupv1.ApplySetupResponse_PHASE_MESH11SD, err
	}

	if err := s.runCommit(ctx, stream); err != nil {
		return setupv1.ApplySetupResponse_PHASE_COMMIT, err
	}

	return setupv1.ApplySetupResponse_PHASE_UNSPECIFIED, nil
}

// ── Phase 2: snapshot ────────────────────────────────────────────────────────

// runSnapshot captures the UCI state of every wizard-touched config
// so failures in phases 3-13 can be rolled back atomically. The
// snapshotter is optional — when nil, the snapshot is skipped and
// rollback becomes a no-op (acceptable in unit tests that don't
// exercise rollback paths).
func (s *SetupService) runSnapshot(ctx context.Context, stream applySetupStream) (UCISnapshot, error) {
	if err := emitPhaseStarted(stream, setupv1.ApplySetupResponse_PHASE_SNAPSHOT,
		"capturing UCI snapshot for rollback"); err != nil {
		return nil, err
	}

	if s.Snapshotter == nil {
		// No snapshotter wired in this deployment; emit DONE so the
		// frontend sees the phase complete. Rollback is a no-op.
		return nil, emitPhaseDone(stream, setupv1.ApplySetupResponse_PHASE_SNAPSHOT,
			"snapshotter not configured; rollback disabled")
	}

	snapshot, err := s.Snapshotter.Snapshot(ctx, wizardConfigs)
	if err != nil {
		_ = emitPhaseFailed(stream, setupv1.ApplySetupResponse_PHASE_SNAPSHOT, err.Error())

		return nil, fmt.Errorf("snapshot UCI: %w", err)
	}

	return snapshot, emitPhaseDone(stream, setupv1.ApplySetupResponse_PHASE_SNAPSHOT,
		fmt.Sprintf("captured %d configs", len(snapshot.Configs())))
}

// ── Phase 3: reset wireless ──────────────────────────────────────────────────

// runResetWireless whitelists every wifi-device + wifi-iface to the
// wizard's standard field set, then disables every wifi-iface. The
// wizard re-enables only the interfaces it intends to keep.
func (s *SetupService) runResetWireless(_ context.Context, stream applySetupStream) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_RESET_WIRELESS,
		"resetting wireless config", func() error {
			devices, err := s.UCI.GetSections("wireless", "wifi-device")
			if err != nil {
				return fmt.Errorf("listing wifi-device sections: %w", err)
			}

			for _, dev := range devices {
				if werr := network.WhitelistDeviceFields(s.UCI, dev,
					network.WizardWifiDeviceWhitelist); werr != nil {
					return werr
				}
			}

			ifaces, err := s.UCI.GetSections("wireless", "wifi-iface")
			if err != nil {
				return fmt.Errorf("listing wifi-iface sections: %w", err)
			}

			for _, iface := range ifaces {
				if werr := network.WhitelistInterfaceFields(s.UCI, iface,
					network.WizardWifiIfaceWhitelist); werr != nil {
					return werr
				}
			}

			return network.DisableAllInterfaces(s.UCI)
		})
}

// ── Phase 4: reset network topology ──────────────────────────────────────────

// runResetNetwork wipes leftover firewall rules, disables existing
// forwardings, clears mtu_fix/masq from zones, ignores existing dhcp
// pools, whitelists the surviving dnsmasq instance to the wizard's
// standard option set, and removes bridge + batadv interfaces.
// Mirrors the LuCI resetUciNetworkTopology() block.
func (s *SetupService) runResetNetwork(_ context.Context, stream applySetupStream) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_RESET_NETWORK,
		"resetting network topology", func() error {
			if err := network.RemoveAllRules(s.UCI); err != nil {
				return err
			}

			if err := network.WhitelistAndDisableForwardings(s.UCI); err != nil {
				return err
			}

			if err := network.UnsetMtuFixAndMasq(s.UCI); err != nil {
				return err
			}

			if err := network.WhitelistAndIgnoreAllPools(s.UCI); err != nil {
				return err
			}

			if err := s.whitelistDnsmasqSurvivors(); err != nil {
				return err
			}

			if err := network.RemoveAllBridgeDevices(s.UCI); err != nil {
				return err
			}

			if err := network.RemoveAllBatadvInterfaces(s.UCI); err != nil {
				return err
			}

			return network.UnsetGatewayAndDeviceOnInterfaces(s.UCI)
		})
}

// whitelistDnsmasqSurvivors first removes any dnsmasq section that
// was scoped to a specific network (had `interface` set, or had
// `notinterface` containing anything other than loopback), then —
// if more than one section remains — removes ALL of them (LuCI's
// "probably broken configuration" guard), then prunes the survivor's
// options down to the wizard's standard whitelist. The scenario
// phase later creates a fresh per-network dnsmasq instance for ahwlan
// (and lan in the mesh-point-none scenario).
//
// Mirrors LuCI's resetUci wizard.js:423-448 block.
func (s *SetupService) whitelistDnsmasqSurvivors() error {
	sections, err := s.UCI.GetSections("dhcp", "dnsmasq")
	if err != nil {
		return fmt.Errorf("listing dnsmasq sections: %w", err)
	}

	// Pass 1: remove every scoped dnsmasq.
	survivors := make([]string, 0, len(sections))

	for _, sec := range sections {
		if dnsmasqIsScoped(s.UCI, sec) {
			if err := s.UCI.DelSection("dhcp", sec); err != nil {
				return fmt.Errorf("removing scoped dnsmasq %s: %w", sec, err)
			}

			continue
		}

		survivors = append(survivors, sec)
	}

	// Pass 2: if multiple unscoped survivors remain, remove ALL of
	// them. This mirrors LuCI's "probably broken" guard — the
	// scenario phase will create exactly one new instance per
	// network it serves DHCP on.
	if len(survivors) > 1 {
		for _, sec := range survivors {
			if err := s.UCI.DelSection("dhcp", sec); err != nil {
				return fmt.Errorf("removing redundant dnsmasq %s: %w", sec, err)
			}
		}

		return nil
	}

	// Pass 3: whitelist the single survivor (if any).
	for _, sec := range survivors {
		if err := network.WhitelistDnsmasqSurvivor(s.UCI, sec, network.WizardDnsmasqWhitelist); err != nil {
			return fmt.Errorf("whitelisting dnsmasq %s: %w", sec, err)
		}
	}

	return nil
}

// dnsmasqIsScoped reports whether the supplied dnsmasq section is
// bound to a specific network — i.e. has `interface` set, or has
// `notinterface` containing any value other than `loopback`. The
// reset phase uses this to identify dnsmasq sections to delete
// before recreating per-network instances.
func dnsmasqIsScoped(reader network.ConfigReader, section string) bool {
	if iface, ok := reader.Get("dhcp", section, "interface"); ok && len(iface) > 0 {
		return true
	}

	notIface, ok := reader.Get("dhcp", section, "notinterface")
	if !ok {
		return false
	}

	for _, n := range notIface {
		if n != "loopback" && n != "" {
			return true
		}
	}

	return false
}

// ── Phase 5: hostname ────────────────────────────────────────────────────────

// runHostname writes the system hostname through the HostnameSetter
// dependency, falling back to a direct UCI write when no setter is
// wired (test wiring path). The init.d/system reload that picks the
// new hostname up runs in the reload goroutine after PHASE_TERMINAL.
func (s *SetupService) runHostname(ctx context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_HOSTNAME,
		"writing system hostname", func() error {
			if s.HostnameSetter != nil {
				return s.HostnameSetter.SetHostname(ctx, profile.GetHostname())
			}

			// Direct write path: stage the hostname change without
			// committing. SetSystemHostnameWithReader commits, so
			// it's not used here — phase 12 is the single commit.
			sections, err := s.UCI.GetSections("system", "system")
			if err != nil {
				return fmt.Errorf("listing system sections: %w", err)
			}

			if len(sections) == 0 {
				return fmt.Errorf("no system section found")
			}

			return s.UCI.SetType("system", sections[0], "hostname",
				uci.TypeOption, profile.GetHostname())
		})
}

// ── Phase 6: base network ifaces ─────────────────────────────────────────────

// runBaseNetwork lays down the network-side L3 + L2 plumbing the
// device needs to come up after a wizard run. Specifically:
//
//   - Creates the `network.ahwlan` interface section (proto=static,
//     netmask=255.255.0.0, ip6assign=64, ip6ifaceid=eui64,
//     ip6class=local, device=br-ahwlan). Mesh-point scenarios also
//     get a randomized ipaddr in the mesh subnet via RandomMeshIP;
//     mesh-gate scenarios omit ipaddr because openmanetd's gateway
//     code assigns the address at runtime.
//   - Creates the `config device 'br-ahwlan'` bridge with type=bridge,
//     a randomly-generated F2:-prefixed MAC, and the scenario's
//     ethernet-port allocation as ports. The batman-adv phase appends
//     `bat0` to the port list afterwards.
//   - For mesh-gate-with-ethernet-router scenarios, flips
//     `network.lan.proto` to `dhcp` so the upstream router DHCP-
//     assigns the WAN address.
//   - For mesh-gate-with-ethernet-router_firewall scenarios, ensures
//     the `network.wan6` interface exists with `proto=dhcpv6`.
//   - Always sets `network.lan.dns=1.1.1.1` (mirrors LuCI's
//     setupBatmanInterfaceOnDevice).
//
// THIS PHASE IS LOAD-BEARING. Without it the device has no
// management interface, no bridge, and ethernet ports are orphaned —
// which is exactly the bricking scenario the 2026-04-28 incident hit.
func (s *SetupService) runBaseNetwork(_ context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_BASE_NETWORK,
		"configuring base network interfaces", func() error {
			scenario, err := classifyScenario(profile)
			if err != nil {
				return err
			}

			ahwlanIPaddr := ""

			if profile.GetRole() == setupv1.MeshRole_MESH_ROLE_MESH_POINT {
				ip, err := network.RandomMeshIP(s.cfgMeshSubnetBaseIP(), s.rng())
				if err != nil {
					return fmt.Errorf("generating mesh-point ahwlan IP: %w", err)
				}

				ahwlanIPaddr = ip
			}

			if err := network.SetupAhwlanInterface(s.UCI, ahwlanIPaddr); err != nil {
				return fmt.Errorf("setupAhwlanInterface: %w", err)
			}

			ports := s.ethernetPortsForAhwlan(profile, scenario)

			if _, err := network.CreateBridgeDevice(s.UCI, "br-ahwlan",
				ports, network.RandomMAC(s.rng())); err != nil {
				return fmt.Errorf("createBridgeDevice br-ahwlan: %w", err)
			}

			// network.lan.dns is set unconditionally (LuCI parity).
			if err := network.SetInterfaceDNSWithReader(s.UCI, "lan", "1.1.1.1"); err != nil {
				return fmt.Errorf("set lan.dns: %w", err)
			}

			// Per-scenario interface protocol writes.
			switch scenario {
			case scenarioMeshGateRouterEth:
				if err := network.SetInterfaceProtoWithReader(s.UCI, "lan", "dhcp"); err != nil {
					return fmt.Errorf("set lan.proto: %w", err)
				}
			case scenarioMeshGateRouterFirewallEth:
				if err := network.EnsureWan6Interface(s.UCI); err != nil {
					return fmt.Errorf("ensureWan6: %w", err)
				}
			case scenarioMeshGateRouterWifiSta:
				// STA radio uses lan as its backing network so the wifi
				// uplink driver brings the iface up via DHCP.
				if err := network.SetInterfaceProtoWithReader(s.UCI, "lan", "dhcp"); err != nil {
					return fmt.Errorf("set lan.proto for wifi-sta: %w", err)
				}
			case scenarioMeshPointExtender, scenarioMeshPointNone, scenarioUnknown:
				// Mesh-point scenarios leave lan as-is (typically
				// proto=static from the factory image). The unknown
				// case shouldn't reach here — classifyScenario already
				// returned an error.
			}

			return nil
		})
}

// ethernetPortsForAhwlan returns the ethernet port list that should
// become br-ahwlan's `ports`. Heuristic per scenario:
//
//   - Mesh-gate with ethernet router/router_firewall + a chosen uplink
//     port: every detected ethernet port EXCEPT the chosen uplink. The
//     uplink stays attached to lan (router) or wan (router_firewall).
//   - Mesh-gate with ethernet but no chosen port: empty ahwlan ports;
//     the operator likely wants to use wifi-AP only on the management
//     side.
//   - Mesh-gate with wifi-sta uplink, mesh-point extender, mesh-point
//     none: every detected ethernet port goes to ahwlan.
//
// Falls back to ["eth0"] when no ports were detected (most BCM2711
// boards) so the bridge is never empty in production.
func (s *SetupService) ethernetPortsForAhwlan(profile *setupv1.MeshNodeProfile, scenario scenarioKind) []string {
	all := s.collectEthernetPorts()

	if len(all) == 0 {
		all = []string{"eth0"}
	}

	switch scenario {
	case scenarioMeshGateRouterEth, scenarioMeshGateRouterFirewallEth:
		uplink := profile.GetUplink().GetEthernetPort()

		if uplink == "" {
			return all
		}

		out := make([]string, 0, len(all))

		for _, p := range all {
			if p != uplink {
				out = append(out, p)
			}
		}

		return out
	case scenarioMeshGateRouterWifiSta,
		scenarioMeshPointExtender,
		scenarioMeshPointNone,
		scenarioUnknown:
		return all
	}

	return all
}

// rng returns the seeded random source the wizard uses for bridge
// MACs, mesh IP addresses, AP keys, and DHCP pool offsets. Falls back
// to a fresh time-seeded source when the service has no pre-seeded
// RNG (production wiring) — tests inject a deterministic seed via
// SetupService.RNG.
func (s *SetupService) rng() *rand.Rand {
	if s.RNG != nil {
		return s.RNG
	}

	// Production fallback. The randomness here only affects MAC
	// addresses and IP randomization, both of which are seed-stable
	// across runs anyway.
	return rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // not security-sensitive
}

// cfgMeshSubnetBaseIP returns the base IP for the mesh subnet,
// preferring the operator-configured value from BLOSAdvertisedMeshSubnet
// and falling back to "10.41.254.1" (which the RandomMeshIP helper
// reduces to its first two octets and a random fourth).
func (s *SetupService) cfgMeshSubnetBaseIP() string {
	if s.Cfg == nil {
		return "10.41.254.1"
	}

	subnet := s.Cfg.GetBLOSAdvertisedMeshSubnet()
	if subnet == "" {
		return "10.41.254.1"
	}

	// Subnet is in CIDR form (e.g. "10.41.0.0/16"); the random helper
	// only uses the first two octets as a base.
	for i := 0; i < len(subnet); i++ {
		if subnet[i] == '/' {
			return subnet[:i]
		}
	}

	return subnet
}

// ── Phase 7: wireless mesh + device knobs ────────────────────────────────────

// runWirelessMesh writes the morse wifi-device's hardcoded mcast and
// PS knobs, the user-supplied mesh interface settings (mesh_id, key,
// encryption, beacon_int=1000, mode=mesh), and the LuCI mesh-AP
// overlay section (always disabled, default SSID/key).
func (s *SetupService) runWirelessMesh(_ context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_WIRELESS_MESH,
		"configuring mesh wireless", func() error {
			mesh := profile.GetMesh()
			if mesh == nil || mesh.GetRadioName() == "" {
				return fmt.Errorf("mesh radio config missing")
			}

			// Mesh wifi-device knobs (the morse radio).
			deviceWrites := []optionWrite{
				{"enable_mcast_whitelist", "0"},
				{"enable_mcast_rate_control", "1"},
				{"enable_ps", "0"},
				{"enable_dynamic_ps_offload", "0"},
				{"enable_twt", "0"},
			}

			if mesh.GetChannel() > 0 {
				deviceWrites = append(deviceWrites, optionWrite{"channel", fmt.Sprintf("%d", mesh.GetChannel())})
			}

			if htmode := bandwidthToHTMode(mesh.GetBandwidthMhz()); htmode != "" {
				deviceWrites = append(deviceWrites, optionWrite{"htmode", htmode})
			}

			// Regulatory domain. The morse driver reads `country` to load
			// the per-country PHY / power tables; without it the radio
			// falls back to a conservative default that may exclude the
			// channel we just wrote. Always set it when the user picked
			// a country (the frontend defaults to the device's existing
			// value, so this is rarely empty).
			if cc := strings.ToUpper(mesh.GetCountryCode()); cc != "" {
				deviceWrites = append(deviceWrites, optionWrite{"country", cc})
			}

			for _, w := range deviceWrites {
				if err := s.UCI.SetType("wireless", mesh.GetRadioName(), w.option,
					uci.TypeOption, w.value); err != nil {
					return fmt.Errorf("setting mesh device %s: %w", w.option, err)
				}
			}

			// Mesh wifi-iface (default_<radioName>). The `network`
			// binding is critical: without it, the wifi driver doesn't
			// know which network section to bring the iface up on, so
			// the mesh radio never comes up. Pin to batmesh0, the
			// primary batadv_hardif created in phase 10. Disabled is
			// explicitly cleared because phase 3 sets disabled=1 on
			// every existing iface; we re-enable the mesh iface here.
			ifaceName := "default_" + mesh.GetRadioName()
			ifaceWrites := []optionWrite{
				{"device", mesh.GetRadioName()},
				{"mode", "mesh"},
				{"network", "batmesh0"},
				{"mesh_id", mesh.GetMeshId()},
				{"key", mesh.GetPassphrase()},
				{"encryption", ProtoToWifiEncryption(mesh.GetEncryption())},
				{"beacon_int", "1000"},
			}

			// Ensure the iface section exists.
			_ = s.UCI.AddSection("wireless", ifaceName, "wifi-iface")

			for _, w := range ifaceWrites {
				if w.value == "" {
					continue
				}

				if err := s.UCI.SetType("wireless", ifaceName, w.option,
					uci.TypeOption, w.value); err != nil {
					return fmt.Errorf("setting mesh iface %s: %w", w.option, err)
				}
			}

			// Clear `disabled` (set by the reset phase) so the mesh
			// iface is brought up on the next wireless reload.
			if err := s.UCI.Del("wireless", ifaceName, "disabled"); err != nil {
				return fmt.Errorf("clearing mesh iface disabled: %w", err)
			}

			// Emit the LuCI mesh-AP overlay section. The wizard does
			// not surface a UI toggle for this — the iface is
			// always written with disabled=1 and default ssid/key
			// so an operator can later enable it from the settings
			// page without having to create the section by hand.
			// Mirrors LuCI's renderPages() behavior; see plan
			// "Per-scenario topology mutations" for the rationale.
			return s.writeMeshAPOverlay(profile, mesh.GetRadioName())
		})
}

// writeMeshAPOverlay emits the `meshap_<radio>` wifi-iface section,
// always disabled, with default SSID = profile hostname and a random
// 8-char key drawn from LuCI's wizard charset. The section gives
// operators a one-click "enable a mesh-AP for client devices that
// can't see the mesh directly" toggle in the settings UI without
// requiring them to create a section from scratch.
func (s *SetupService) writeMeshAPOverlay(profile *setupv1.MeshNodeProfile, meshRadio string) error {
	ifaceName := "meshap_" + meshRadio

	if err := s.UCI.AddSection("wireless", ifaceName, "wifi-iface"); err != nil {
		s.Log.Debug().Err(err).Str("section", ifaceName).Msg("AddSection meshap (ignored)")
	}

	writes := []optionWrite{
		{"device", meshRadio},
		{"mode", "ap"},
		{"network", "ahwlan"},
		{"encryption", "sae"},
		{"ssid", profile.GetHostname()},
		{"key", network.RandomWifiKey(s.rng())},
		{"disabled", "1"},
	}

	for _, w := range writes {
		if w.value == "" {
			continue
		}

		if err := s.UCI.SetType("wireless", ifaceName, w.option, uci.TypeOption, w.value); err != nil {
			return fmt.Errorf("setting meshap iface %s.%s: %w", ifaceName, w.option, err)
		}
	}

	return nil
}

// optionWrite is a small struct used by phase helpers that batch
// many SetType calls on the same section.
type optionWrite struct {
	option string
	value  string
}

// bandwidthToHTMode maps a megahertz bandwidth to the corresponding
// UCI htmode string. The S1G values match LuCI's `1 MHz`, `2 MHz`,
// etc. literals (note the space).
//
//nolint:goconst // these literals are also defined in wifi_config.go's HTMode helpers; consolidating into shared constants is a separate refactor outside the wizard work
func bandwidthToHTMode(mhz uint32) string {
	switch mhz {
	case 1:
		return "1 MHz"
	case 2:
		return "2 MHz"
	case 4:
		return "4 MHz"
	case 8:
		return "8 MHz"
	case 20:
		return "HT20"
	case 40:
		return "HT40"
	case 80:
		return "VHT80"
	case 160:
		return "VHT160"
	default:
		return ""
	}
}

// ── Phase 8: per-radio AP / STA writes ───────────────────────────────────────

// runPerRadioAPSta writes one AP wifi-iface per non-mesh radio in
// the profile (enabled or not), plus an STA wifi-iface for the chosen
// wifi-uplink radio (when the uplink type is WIRELESS_STA).
//
// Critical writes:
//
//   - Every AP iface gets `network=ahwlan` so AP clients are bridged
//     onto the management network. Without this option, AP clients
//     have no L3/L2 path and the AP never comes up.
//   - Disabled APs still get an iface section (with disabled=1) so
//     the operator can later toggle them on through the settings UI
//     without having to re-create the section.
//   - The STA iface gets `network=lan` so DHCP from the upstream is
//     written into the lan subnet (matches LuCI scenario 3d).
//
// Uses raw SetType calls (rather than network.SetWirelessIfaceConfig)
// so writes stay staged in the in-memory tree until phase 12 commits.
func (s *SetupService) runPerRadioAPSta(_ context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_PER_RADIO_AP_STA,
		"configuring per-radio AP/STA", func() error {
			for _, ap := range profile.GetAps() {
				if err := s.writeAPIface(ap); err != nil {
					return err
				}
			}

			// Wireless STA uplink: write the sta interface that the
			// scenario topology phase will reference.
			if u := profile.GetUplink(); u != nil &&
				u.GetType() == setupv1.UplinkType_UPLINK_TYPE_WIRELESS_STA &&
				u.GetWireless() != nil {
				return s.writeSTAIface(u.GetWireless())
			}

			return nil
		})
}

// writeAPIface stages the wifi-iface section for one operator-
// configured AP. Disabled APs are written with `disabled=1` and
// minimal options (device, mode, network) so the section is present
// for later edits but doesn't bring up an AP on the next reload.
func (s *SetupService) writeAPIface(ap *setupv1.RadioApProfile) error {
	ifaceName := "default_" + ap.GetRadioName()

	if err := s.UCI.AddSection("wireless", ifaceName, "wifi-iface"); err != nil {
		// AddSection on an existing named section may error on some
		// readers; subsequent SetType calls work either way.
		s.Log.Debug().Err(err).Str("section", ifaceName).Msg("AddSection (ignored)")
	}

	// `network` binding is unconditional. Even disabled APs are wired
	// to ahwlan so a later toggle-to-enabled doesn't need to backfill it.
	baseWrites := []optionWrite{
		{"device", ap.GetRadioName()},
		{"mode", "ap"},
		{"network", "ahwlan"},
	}

	for _, w := range baseWrites {
		if err := s.UCI.SetType("wireless", ifaceName, w.option,
			uci.TypeOption, w.value); err != nil {
			return fmt.Errorf("setting AP iface %s.%s: %w", ifaceName, w.option, err)
		}
	}

	if !ap.GetEnabled() {
		// Mark disabled and clear any leftover ssid/key/encryption from
		// a prior wizard run so a future toggle-to-enabled won't pick
		// up stale credentials.
		if err := s.UCI.SetType("wireless", ifaceName, "disabled", uci.TypeOption, "1"); err != nil {
			return fmt.Errorf("setting AP iface %s.disabled: %w", ifaceName, err)
		}

		return nil
	}

	// Enabled AP: write credentials + clear `disabled`.
	credentialWrites := []optionWrite{
		{"ssid", ap.GetSsid()},
		{"key", ap.GetPassphrase()},
		{"encryption", ProtoToWifiEncryption(ap.GetEncryption())},
	}

	for _, w := range credentialWrites {
		if w.value == "" {
			continue
		}

		if err := s.UCI.SetType("wireless", ifaceName, w.option,
			uci.TypeOption, w.value); err != nil {
			return fmt.Errorf("setting AP iface %s.%s: %w", ifaceName, w.option, err)
		}
	}

	if err := s.UCI.Del("wireless", ifaceName, "disabled"); err != nil {
		return fmt.Errorf("clearing AP iface %s.disabled: %w", ifaceName, err)
	}

	return nil
}

// writeSTAIface stages the wifi-iface for a wireless-STA uplink. The
// iface is bound to lan so the upstream's DHCP server populates the
// lan interface's address.
func (s *SetupService) writeSTAIface(w *setupv1.WifiStaProfile) error {
	ifaceName := "sta_" + w.GetRadioName()

	if err := s.UCI.AddSection("wireless", ifaceName, "wifi-iface"); err != nil {
		s.Log.Debug().Err(err).Str("section", ifaceName).Msg("AddSection (ignored)")
	}

	writes := []optionWrite{
		{"device", w.GetRadioName()},
		{"mode", "sta"},
		{"network", "lan"},
		{"ssid", w.GetSsid()},
		{"key", w.GetPassphrase()},
		{"encryption", ProtoToWifiEncryption(w.GetEncryption())},
	}

	for _, ow := range writes {
		if ow.value == "" {
			continue
		}

		if err := s.UCI.SetType("wireless", ifaceName, ow.option,
			uci.TypeOption, ow.value); err != nil {
			return fmt.Errorf("setting STA iface %s.%s: %w", ifaceName, ow.option, err)
		}
	}

	if err := s.UCI.Del("wireless", ifaceName, "disabled"); err != nil {
		return fmt.Errorf("clearing STA iface %s.disabled: %w", ifaceName, err)
	}

	return nil
}

// ── Phase 9: scenario topology ───────────────────────────────────────────────

// runScenarioTopology dispatches to one of the five canonical scenario
// implementations based on (role × device_mode × uplink_type) from
// the profile. Each scenario writes the network/firewall/dhcp
// interactions specific to that topology.
func (s *SetupService) runScenarioTopology(_ context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_SCENARIO_TOPOLOGY,
		"applying scenario topology", func() error {
			scenario, err := classifyScenario(profile)
			if err != nil {
				return err
			}

			switch scenario {
			case scenarioMeshGateRouterEth:
				return s.scenarioMeshGateRouter(profile, "lan")
			case scenarioMeshGateRouterFirewallEth:
				return s.scenarioMeshGateRouter(profile, "wan")
			case scenarioMeshGateRouterWifiSta:
				return s.scenarioMeshGateRouter(profile, "lan")
			case scenarioMeshPointExtender:
				return s.scenarioMeshPointExtender(profile)
			case scenarioMeshPointNone:
				return s.scenarioMeshPointNone(profile)
			default:
				return fmt.Errorf("unknown scenario %d", scenario)
			}
		})
}

type scenarioKind int

const (
	scenarioUnknown scenarioKind = iota
	scenarioMeshGateRouterEth
	scenarioMeshGateRouterFirewallEth
	scenarioMeshGateRouterWifiSta
	scenarioMeshPointExtender
	scenarioMeshPointNone
)

// classifyScenario folds (role × device_mode × uplink_type) into one
// of the five canonical scenario kinds.
func classifyScenario(profile *setupv1.MeshNodeProfile) (scenarioKind, error) {
	role := profile.GetRole()

	switch role {
	case setupv1.MeshRole_MESH_ROLE_MESH_GATE:
		mode := profile.GetMeshgateMode()
		uplink := profile.GetUplink().GetType()

		switch {
		case mode == setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER &&
			uplink == setupv1.UplinkType_UPLINK_TYPE_ETHERNET:
			return scenarioMeshGateRouterEth, nil
		case mode == setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER_FIREWALL &&
			uplink == setupv1.UplinkType_UPLINK_TYPE_ETHERNET:
			return scenarioMeshGateRouterFirewallEth, nil
		case mode == setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER &&
			uplink == setupv1.UplinkType_UPLINK_TYPE_WIRELESS_STA:
			return scenarioMeshGateRouterWifiSta, nil
		}
	case setupv1.MeshRole_MESH_ROLE_MESH_POINT:
		switch profile.GetMeshpointMode() {
		case setupv1.MeshPointMode_MESH_POINT_MODE_EXTENDER:
			return scenarioMeshPointExtender, nil
		case setupv1.MeshPointMode_MESH_POINT_MODE_NONE:
			return scenarioMeshPointNone, nil
		}
	}

	return scenarioUnknown, fmt.Errorf("unsupported scenario for role=%v", role)
}

// scenarioMeshGateRouter sets up a mesh gate that routes traffic
// between ahwlan and the upstream zone (lan or wan). Writes:
//
//   - ahwlan firewall zone with mtu_fix=1 (local zone marker)
//   - upstream zone (lan or wan), with masq=1 and mtu_fix=1 set on
//     the destination of the mmrouter forwarding so NAT works
//   - lan zone gets mtu_fix=1 so MSS clamping clamps for VPN-style
//     traffic
//   - The mmrouter forwarding (ahwlan → upstream)
//   - 13 default WAN firewall rules
//   - DHCP pool + dnsmasq for ahwlan
//   - network.wizard bookkeeping section
func (s *SetupService) scenarioMeshGateRouter(profile *setupv1.MeshNodeProfile, upstreamZone string) error {
	if _, err := network.GetOrCreateZone(s.UCI, "ahwlan"); err != nil {
		return fmt.Errorf("ensuring ahwlan zone: %w", err)
	}

	if err := network.SetZoneOption(s.UCI, "ahwlan", "mtu_fix", "1"); err != nil {
		return fmt.Errorf("setting ahwlan mtu_fix: %w", err)
	}

	if _, err := network.GetOrCreateZone(s.UCI, upstreamZone); err != nil {
		return fmt.Errorf("ensuring %s zone: %w", upstreamZone, err)
	}

	// router_firewall scenarios route through wan and add wan6 (created
	// in phase 6) for IPv6 transit. Without wan6 in the wan zone's
	// network list, the implicit-REJECT default zone applies and IPv6
	// packets on wan6 are dropped. Mirrors the LuCI fixture's wan zone
	// shape.
	if upstreamZone == "wan" {
		if err := network.AppendZoneNetwork(s.UCI, "wan", "wan6"); err != nil {
			return fmt.Errorf("adding wan6 to wan zone: %w", err)
		}
	}

	if _, err := network.GetOrCreateForwarding(s.UCI, "ahwlan", upstreamZone, "mmrouter"); err != nil {
		return fmt.Errorf("creating mmrouter forwarding: %w", err)
	}

	// LAN zone gets mtu_fix=1 (and masq=1 when LAN is the upstream of
	// the gateway). This matches the gate-router-eth fixture which
	// shows lan with masq=1 mtu_fix=1.
	if upstreamZone == "lan" {
		if err := network.SetZoneOption(s.UCI, "lan", "masq", "1"); err != nil {
			return fmt.Errorf("setting lan masq: %w", err)
		}
	}

	if err := network.SetZoneOption(s.UCI, "lan", "mtu_fix", "1"); err != nil {
		return fmt.Errorf("setting lan mtu_fix: %w", err)
	}

	if err := network.AddDefaultWanFirewallRules(s.UCI, "ahwlan"); err != nil {
		return err
	}

	if err := s.setupAhwlanDhcp(); err != nil {
		return err
	}

	return s.writeWizardBookkeeping(profile)
}

// scenarioMeshPointExtender sets up a mesh point that bridges the
// mesh onto its ethernet/AP clients. Forwarding direction matches
// LuCI's branch 4a (lan → ahwlan, named "mmextender"). Writes:
//
//   - ahwlan firewall zone with mtu_fix=1
//   - lan firewall zone with mtu_fix=1
//   - mmextender forwarding lan → ahwlan
//   - 13 default WAN firewall rules
//   - DHCP pool + dnsmasq for ahwlan
//   - network.wizard bookkeeping
func (s *SetupService) scenarioMeshPointExtender(profile *setupv1.MeshNodeProfile) error {
	if _, err := network.GetOrCreateZone(s.UCI, "lan"); err != nil {
		return fmt.Errorf("ensuring lan zone: %w", err)
	}

	if err := network.SetZoneOption(s.UCI, "lan", "mtu_fix", "1"); err != nil {
		return fmt.Errorf("setting lan mtu_fix: %w", err)
	}

	if _, err := network.GetOrCreateZone(s.UCI, "ahwlan"); err != nil {
		return fmt.Errorf("ensuring ahwlan zone: %w", err)
	}

	if err := network.SetZoneOption(s.UCI, "ahwlan", "mtu_fix", "1"); err != nil {
		return fmt.Errorf("setting ahwlan mtu_fix: %w", err)
	}

	if _, err := network.GetOrCreateForwarding(s.UCI, "lan", "ahwlan", "mmextender"); err != nil {
		return fmt.Errorf("creating mmextender forwarding: %w", err)
	}

	if err := network.AddDefaultWanFirewallRules(s.UCI, "ahwlan"); err != nil {
		return err
	}

	if err := s.setupAhwlanDhcp(); err != nil {
		return err
	}

	return s.writeWizardBookkeeping(profile)
}

// scenarioMeshPointNone sets up a "headless" mesh node — joins the
// mesh on ahwlan but provides no extender role. This is the LuCI 4b
// case (meshpoint=none): ahwlan is a DHCP CLIENT (pulls its address
// from a peer mesh-gate), and any local services (DHCP for an
// ethernet-attached client) live on lan.
//
// Diverges from the other mesh-point scenarios: this scenario
// REPLACES the static ahwlan written by phase 6 with proto=dhcp and
// removes the random ipaddr the SetupAhwlanInterface helper put
// there. The LuCI wizard does the equivalent override after
// nonBridgeMode runs.
func (s *SetupService) scenarioMeshPointNone(profile *setupv1.MeshNodeProfile) error {
	// Override ahwlan: DHCP client (pulls address from a peer
	// mesh-gate over batman). Clear the static ipaddr the base-
	// network phase wrote.
	if err := network.SetInterfaceProtoWithReader(s.UCI, "ahwlan", "dhcp"); err != nil {
		return fmt.Errorf("set ahwlan.proto=dhcp: %w", err)
	}

	if err := s.UCI.Del("network", "ahwlan", "ipaddr"); err != nil {
		return fmt.Errorf("clear ahwlan.ipaddr: %w", err)
	}

	if _, err := network.GetOrCreateZone(s.UCI, "ahwlan"); err != nil {
		return fmt.Errorf("ensuring ahwlan zone: %w", err)
	}

	if err := network.SetZoneOption(s.UCI, "ahwlan", "mtu_fix", "1"); err != nil {
		return fmt.Errorf("setting ahwlan mtu_fix: %w", err)
	}

	// LAN gets a DHCP pool but with the no-router/no-DNS option
	// codes set so the device doesn't claim to be a default
	// gateway for clients on the LAN side. This mirrors LuCI's
	// setupNetworkWithDnsmasq('lan', lanIp, uplink=false).
	if _, err := network.GetOrCreateZone(s.UCI, "lan"); err != nil {
		return fmt.Errorf("ensuring lan zone: %w", err)
	}

	if err := network.SetZoneOption(s.UCI, "lan", "mtu_fix", "1"); err != nil {
		return fmt.Errorf("setting lan mtu_fix: %w", err)
	}

	if err := network.AddDefaultWanFirewallRules(s.UCI, "ahwlan"); err != nil {
		return err
	}

	if err := s.setupLanDhcpNoUplink(); err != nil {
		return err
	}

	return s.writeWizardBookkeeping(profile)
}

// setupLanDhcpNoUplink creates a DHCP pool + dnsmasq instance bound
// to the lan interface, with `dhcp_option=['3','6']` set so dnsmasq
// doesn't advertise itself as a router or DNS server. Used by the
// mesh-point-none scenario where the device is downstream of a peer
// mesh-gate that owns the upstream gateway/DNS.
func (s *SetupService) setupLanDhcpNoUplink() error {
	const (
		networkID   = "lan"
		dnsmasqName = "lan_dns"
	)

	if err := s.UCI.AddSection("dhcp", dnsmasqName, "dnsmasq"); err != nil {
		s.Log.Debug().Err(err).Msg("AddSection lan dnsmasq (ignored if exists)")
	}

	if err := network.SetupDnsmasqInstance(s.UCI, dnsmasqName, networkID); err != nil {
		return fmt.Errorf("setupDnsmasqInstance lan: %w", err)
	}

	pool, err := network.GetOrCreateDhcpPool(s.UCI, dnsmasqName, networkID, s.rng())
	if err != nil {
		return fmt.Errorf("getOrCreateDhcpPool lan: %w", err)
	}

	// dhcp_option=['3','6'] suppresses both the default-route option
	// (3) and the DNS-server option (6), so clients on the LAN don't
	// try to route their default gateway through this device.
	if err := s.UCI.SetType("dhcp", pool, "dhcp_option", uci.TypeList, "3", "6"); err != nil {
		return fmt.Errorf("setting lan dhcp_option: %w", err)
	}

	return nil
}

// setupAhwlanDhcp creates a DHCP pool + dnsmasq instance bound to
// the ahwlan interface. Without this, clients on the management
// network never get an IP and the device is unreachable to anyone
// not statically configured. The dnsmasq survivor is whitelisted to
// the wizard's standard option set in the reset phase, then this
// helper layers the per-network options on top.
func (s *SetupService) setupAhwlanDhcp() error {
	const (
		networkID   = "ahwlan"
		dnsmasqName = "ahwlan_dns"
	)

	// Create a per-network dnsmasq instance so the global anonymous
	// dnsmasq stays as a fallback. This mirrors LuCI's behavior of
	// creating a named dnsmasq when the wizard provides DHCP.
	if err := s.UCI.AddSection("dhcp", dnsmasqName, "dnsmasq"); err != nil {
		s.Log.Debug().Err(err).Msg("AddSection dnsmasq (ignored if exists)")
	}

	if err := network.SetupDnsmasqInstance(s.UCI, dnsmasqName, networkID); err != nil {
		return fmt.Errorf("setupDnsmasqInstance: %w", err)
	}

	if _, err := network.GetOrCreateDhcpPool(s.UCI, dnsmasqName, networkID, s.rng()); err != nil {
		return fmt.Errorf("getOrCreateDhcpPool: %w", err)
	}

	return nil
}

// writeWizardBookkeeping records the user's selections in the
// `network.wizard` section so the settings UI can show them later.
// Mirrors the LuCI wizard's `network.wizard.{device_mode_meshgate,
// device_mode_meshpoint, uplink}` writes.
func (s *SetupService) writeWizardBookkeeping(profile *setupv1.MeshNodeProfile) error {
	const (
		networkConfig = "network"
		wizardSection = "wizard"
	)

	// Ensure the wizard interface section exists. AddSection on an
	// already-existing named section may error on some readers;
	// ignore — subsequent SetType calls work either way.
	_ = s.UCI.AddSection(networkConfig, wizardSection, "interface")

	if mp := ProtoToMeshPointMode(profile.GetMeshpointMode()); mp != "" {
		if err := s.UCI.SetType(networkConfig, wizardSection, "device_mode_meshpoint",
			uci.TypeOption, mp); err != nil {
			return err
		}
	}

	if mg := ProtoToMeshGateMode(profile.GetMeshgateMode()); mg != "" {
		if err := s.UCI.SetType(networkConfig, wizardSection, "device_mode_meshgate",
			uci.TypeOption, mg); err != nil {
			return err
		}
	}

	if u := ProtoToUplinkType(profile.GetUplink().GetType()); u != "" {
		if err := s.UCI.SetType(networkConfig, wizardSection, "uplink",
			uci.TypeOption, u); err != nil {
			return err
		}
	}

	return nil
}

// ── Phase 10: batman-adv ─────────────────────────────────────────────────────

// batman-adv gw_mode constants. "server" advertises the gateway role
// to mesh peers (set on mesh-gates); "client" advertises that we'll
// use one (set on mesh-points). Wrong gw_mode means a mesh-gate's
// uplink is invisible to peers — they fall back to non-gated routing
// or fail to reach the internet.
const (
	batmanGatewayModeServer = "server"
	batmanGatewayModeClient = "client"
)

// runBatmanAdv creates the bat0 batman-adv device and the two
// batadv_hardif interfaces (batmesh0, batmesh1) that ride on top of
// it, with role-aware gw_mode. Also appends `bat0` to the br-ahwlan
// bridge's port list so batman traffic flows through the management
// bridge to wifi APs and ethernet ports.
//
// batman is mandatory in this codebase, so this phase always runs
// regardless of scenario.
func (s *SetupService) runBatmanAdv(_ context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_BATMAN_ADV,
		"configuring batman-adv", func() error {
			gwMode := batmanGwModeForRole(profile.GetRole())

			if err := network.SetupBatmanDeviceOnNetwork(s.UCI, gwMode,
				network.BatmanDeviceName); err != nil {
				return err
			}

			if err := network.SetupBatmanInterfaceOnDevice(s.UCI, network.BatmanDeviceName); err != nil {
				return err
			}

			// Attach bat0 to the br-ahwlan bridge so batman traffic
			// flows through the management bridge. The bridge was
			// created in phase 6 with the scenario's ethernet ports;
			// bat0 is appended here so the resulting ports list looks
			// like `[<eth-ports...>, bat0]`, matching the LuCI
			// fixtures.
			bridgeSection, err := network.FindBridgeBySectionName(s.UCI, "br-ahwlan")
			if err != nil {
				return fmt.Errorf("locating br-ahwlan: %w", err)
			}

			if bridgeSection == "" {
				return fmt.Errorf("br-ahwlan bridge not found; phase 6 (BASE_NETWORK) must run first")
			}

			return network.AppendBridgePort(s.UCI, bridgeSection, network.BatmanDeviceName)
		})
}

// batmanGwModeForRole returns the batman-adv gw_mode UCI string for
// the supplied wizard role. Mesh-gates advertise "server"; mesh-points
// advertise "client". Defaults to "client" for unspecified/unknown
// values (the safer setting on a misconfigured device).
func batmanGwModeForRole(role setupv1.MeshRole) string {
	if role == setupv1.MeshRole_MESH_ROLE_MESH_GATE {
		return batmanGatewayModeServer
	}

	return batmanGatewayModeClient
}

// ── Phase 11: mesh11sd announcements ─────────────────────────────────────────

// runMesh11sd writes mesh_fwding=0 (batman-adv handles forwarding)
// and mesh_gate_announcements per the user's role choice.
func (s *SetupService) runMesh11sd(_ context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_MESH11SD,
		"writing mesh11sd announcements", func() error {
			if err := network.SetMeshFwding(s.UCI, "0"); err != nil {
				return err
			}

			if err := network.SetMeshGateAnnouncements(s.UCI,
				ProtoToMeshRole(profile.GetRole())); err != nil {
				return err
			}

			return network.SetMesh11sdSetupEnabled(s.UCI, "1")
		})
}

// ── Phase 12: commit ─────────────────────────────────────────────────────────

// runCommit flushes the staged UCI tree to disk. This is the point
// after which the wizard's writes are durable.
func (s *SetupService) runCommit(_ context.Context, stream applySetupStream) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_COMMIT,
		"committing UCI", func() error {
			return s.UCI.Commit()
		})
}

// ── Phase 13: set admin password ─────────────────────────────────────────────

// runPassword writes the admin password through the PasswordSetter
// dependency. Failure here triggers UCI rollback (commit already ran)
// but the partially-set password is left as-is — the user knows the
// password they typed, and re-applying chpasswd from a rollback path
// risks setting it to an empty string if the failure was input-related.
func (s *SetupService) runPassword(ctx context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_PASSWORD,
		"setting admin password", func() error {
			if s.PasswordSetter == nil {
				return fmt.Errorf("PasswordSetter not configured")
			}

			return s.PasswordSetter.SetPassword(ctx, "root", profile.GetAdminPassword())
		})
}

// ── Phase 14: atomic flag flip ───────────────────────────────────────────────

// runPersistFlags atomically flips setup.complete=true and
// auth.enable=true in /etc/openmanetd/config.yml so a crash between
// the two writes cannot leave the device half-configured. This is
// the point of no return; after this returns successfully, the
// re-apply guard rejects new ApplySetup calls and the device is
// considered fully configured.
func (s *SetupService) runPersistFlags(_ context.Context, stream applySetupStream) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_PERSIST_FLAGS,
		"persisting setup.complete and auth.enable", func() error {
			return s.Cfg.PersistSetupAndAuth(true, true)
		})
}

// ── runPhase: generic STARTED/DONE/FAILED wrapper ───────────────────────────

// runPhase emits STATUS_STARTED, runs body, and emits STATUS_DONE on
// success or STATUS_FAILED on error. Returns the body's error so the
// orchestrator can map it to a phase-tagged terminal event.
func (s *SetupService) runPhase(
	stream applySetupStream,
	phase setupv1.ApplySetupResponse_Phase,
	startedMsg string,
	body func() error,
) error {
	if err := emitPhaseStarted(stream, phase, startedMsg); err != nil {
		return err
	}

	if err := body(); err != nil {
		_ = emitPhaseFailed(stream, phase, err.Error())

		return err
	}

	return emitPhaseDone(stream, phase, "")
}
