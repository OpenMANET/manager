package handlers

import (
	"context"
	"fmt"
	"strings"

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

	if err := s.runBatmanAdv(ctx, stream); err != nil {
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
// pools, and removes bridge + batadv interfaces. Mirrors the LuCI
// resetUciNetworkTopology() block.
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

			if err := network.RemoveAllBridgeDevices(s.UCI); err != nil {
				return err
			}

			if err := network.RemoveAllBatadvInterfaces(s.UCI); err != nil {
				return err
			}

			return network.UnsetGatewayAndDeviceOnInterfaces(s.UCI)
		})
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

// runBaseNetwork sets up the lan, ahwlan, and wan network sections
// to their wizard-default state. Each scenario's topology phase
// (phase 9) layers scenario-specific writes on top.
//
// This phase is currently a placeholder that emits the phase events
// and returns success. The detailed per-scenario writes land in the
// scenario topology phase. Splitting the writes between phase 6 and
// phase 9 was a LuCI convention; merging them simplifies the Go
// implementation without changing observable behavior.
func (s *SetupService) runBaseNetwork(_ context.Context, stream applySetupStream, _ *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_BASE_NETWORK,
		"configuring base network interfaces", func() error {
			// Phase 9 (scenario topology) does the per-scenario work.
			// This phase exists in the proto enum so the frontend can
			// render a per-phase progress dot, but the actual UCI
			// writes happen in phase 9.
			return nil
		})
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

			// Mesh wifi-iface (default_<radioName>).
			ifaceName := "default_" + mesh.GetRadioName()
			ifaceWrites := []optionWrite{
				{"device", mesh.GetRadioName()},
				{"mode", "mesh"},
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

			return nil
		})
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

// runPerRadioAPSta writes one AP wifi-iface per enabled non-mesh
// radio in the profile, plus an STA wifi-iface for the chosen wifi-
// uplink radio (when the uplink type is WIRELESS_STA).
//
// Uses raw SetType calls (rather than network.SetWirelessIfaceConfig)
// so writes stay staged in the in-memory tree until phase 12 commits.
func (s *SetupService) runPerRadioAPSta(_ context.Context, stream applySetupStream, profile *setupv1.MeshNodeProfile) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_PER_RADIO_AP_STA,
		"configuring per-radio AP/STA", func() error {
			for _, ap := range profile.GetAps() {
				if !ap.GetEnabled() {
					continue
				}

				ifaceName := "default_" + ap.GetRadioName()

				_ = s.UCI.AddSection("wireless", ifaceName, "wifi-iface")

				writes := []optionWrite{
					{"device", ap.GetRadioName()},
					{"mode", "ap"},
					{"ssid", ap.GetSsid()},
					{"key", ap.GetPassphrase()},
					{"encryption", ProtoToWifiEncryption(ap.GetEncryption())},
				}

				for _, w := range writes {
					if w.value == "" {
						continue
					}

					if err := s.UCI.SetType("wireless", ifaceName, w.option,
						uci.TypeOption, w.value); err != nil {
						return fmt.Errorf("setting AP iface %s.%s: %w", ifaceName, w.option, err)
					}
				}
			}

			// Wireless STA uplink: write the sta interface that the
			// scenario topology phase will reference.
			if u := profile.GetUplink(); u != nil &&
				u.GetType() == setupv1.UplinkType_UPLINK_TYPE_WIRELESS_STA &&
				u.GetWireless() != nil {
				w := u.GetWireless()
				ifaceName := "sta_" + w.GetRadioName()

				_ = s.UCI.AddSection("wireless", ifaceName, "wifi-iface")

				writes := []optionWrite{
					{"device", w.GetRadioName()},
					{"mode", "sta"},
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
			}

			return nil
		})
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
// between ahwlan and the upstream zone (lan or wan). The firewall
// forwarding from ahwlan to upstream is named "mmrouter".
func (s *SetupService) scenarioMeshGateRouter(profile *setupv1.MeshNodeProfile, upstreamZone string) error {
	if _, err := network.GetOrCreateZone(s.UCI, "ahwlan"); err != nil {
		return fmt.Errorf("ensuring ahwlan zone: %w", err)
	}

	if _, err := network.GetOrCreateZone(s.UCI, upstreamZone); err != nil {
		return fmt.Errorf("ensuring %s zone: %w", upstreamZone, err)
	}

	if _, err := network.GetOrCreateForwarding(s.UCI, "ahwlan", upstreamZone, "mmrouter"); err != nil {
		return fmt.Errorf("creating mmrouter forwarding: %w", err)
	}

	if err := network.AddDefaultWanFirewallRules(s.UCI, "ahwlan"); err != nil {
		return err
	}

	return s.writeWizardBookkeeping(profile)
}

// scenarioMeshPointExtender sets up a mesh point that bridges the
// mesh onto its lan-attached AP clients. Forwarding direction is
// lan → ahwlan, named "mmextender".
func (s *SetupService) scenarioMeshPointExtender(profile *setupv1.MeshNodeProfile) error {
	if _, err := network.GetOrCreateZone(s.UCI, "lan"); err != nil {
		return fmt.Errorf("ensuring lan zone: %w", err)
	}

	if _, err := network.GetOrCreateZone(s.UCI, "ahwlan"); err != nil {
		return fmt.Errorf("ensuring ahwlan zone: %w", err)
	}

	if _, err := network.GetOrCreateForwarding(s.UCI, "lan", "ahwlan", "mmextender"); err != nil {
		return fmt.Errorf("creating mmextender forwarding: %w", err)
	}

	if err := network.AddDefaultWanFirewallRules(s.UCI, "ahwlan"); err != nil {
		return err
	}

	return s.writeWizardBookkeeping(profile)
}

// scenarioMeshPointNone sets up a mesh-only node with no uplink,
// where ahwlan is the management interface itself.
func (s *SetupService) scenarioMeshPointNone(profile *setupv1.MeshNodeProfile) error {
	if _, err := network.GetOrCreateZone(s.UCI, "ahwlan"); err != nil {
		return fmt.Errorf("ensuring ahwlan zone: %w", err)
	}

	if err := network.AddDefaultWanFirewallRules(s.UCI, "ahwlan"); err != nil {
		return err
	}

	return s.writeWizardBookkeeping(profile)
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

// batmanGatewayModeClient is the batman-adv gw_mode value the wizard
// always writes. gw_mode "server" advertises the gateway role to mesh
// peers; "client" advertises that we'll use one. The mesh11sd phase
// decides which by setting mesh_gate_announcements based on role.
const batmanGatewayModeClient = "client"

// runBatmanAdv creates the bat0 batman-adv device and the two
// batadv_hardif interfaces (batmesh0, batmesh1) that ride on top of
// it. batman is mandatory in this codebase, so this phase always
// runs regardless of scenario.
func (s *SetupService) runBatmanAdv(_ context.Context, stream applySetupStream) error {
	return s.runPhase(stream, setupv1.ApplySetupResponse_PHASE_BATMAN_ADV,
		"configuring batman-adv", func() error {
			if err := network.SetupBatmanDeviceOnNetwork(s.UCI, batmanGatewayModeClient,
				network.BatmanDeviceName); err != nil {
				return err
			}

			return network.SetupBatmanInterfaceOnDevice(s.UCI, network.BatmanDeviceName)
		})
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
