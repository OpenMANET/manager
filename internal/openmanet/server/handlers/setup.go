package handlers

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	setupv1 "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1"
	wificonfigv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	"github.com/openmanet/openmanetd/internal/auth"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/iwinfo"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/system"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

// HostnameSetter writes the system hostname. Production wraps
// network.SetSystemHostnameWithReader; tests substitute a fake.
type HostnameSetter interface {
	SetHostname(ctx context.Context, hostname string) error
}

// DefaultHostnameSetter is the production HostnameSetter. It writes
// `system.@system[0].hostname` via the supplied UCI reader and
// commits.
type DefaultHostnameSetter struct {
	Reader network.ConfigReader
}

// SetHostname implements HostnameSetter.
func (d *DefaultHostnameSetter) SetHostname(_ context.Context, hostname string) error {
	return network.SetSystemHostnameWithReader(hostname, d.Reader)
}

// hostnameRFC1123 matches an RFC 1123 hostname label: 1-63 chars,
// alphanumeric, hyphens allowed internally, must NOT start or end
// with a hyphen. Mirrors the proto validator pattern so the handler
// can re-check before writing UCI as defense-in-depth.
var hostnameRFC1123 = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)

// factoryHostnamePattern matches the OpenMANET firmware's default
// hostname (`BCM2711-XXXX`) so GetSetupStatus can flag a fresh
// device as "not yet configured" via current_hostname inspection.
var factoryHostnamePattern = regexp.MustCompile(`^BCM\d+-[0-9a-fA-F]+$`)

// SetupService implements the SetupService ConnectRPC service. It
// runs the first-boot setup wizard. Both RPCs are exempt from auth
// (see auth/middleware.go isAPISkipPath); the middleware additionally
// gates on `setup.enabled=true` and `setup.complete=false`. Phase 0
// of ApplySetup re-applies these checks as defense-in-depth.
type SetupService struct {
	Cfg            *config.Config
	Log            zerolog.Logger
	UCI            network.ConfigReader
	PasswordSetter auth.PasswordSetter
	HostnameSetter HostnameSetter
	Reloader       system.ServiceReloader
	Iwinfo         iwinfo.IwinfoProvider
	Interfaces     InterfaceProvider
	// RNG seeds RandomMAC, RandomDhcpStart, RandomMeshIP, RandomWifiKey.
	// Production passes a freshly-seeded *rand.Rand; tests pass a
	// deterministically-seeded one.
	RNG *rand.Rand
	// Now returns the current time. Override for deterministic tests.
	Now func() time.Time

	// mu serializes ApplySetup so two browser tabs racing the wizard
	// see exactly one "win" and one CodeFailedPrecondition.
	mu sync.Mutex
}

// ─── Enum translators ─────────────────────────────────────────────────────────
//
// Each defined enum value has a string_name annotation in the .proto. The
// translators below carry that mapping into Go. Round-trip tests in
// setup_test.go assert the mapping is symmetric and unknown UCI strings
// produce the *_UNSPECIFIED variant.

// ProtoToMeshRole maps the wizard MeshRole enum onto the UCI
// `mesh11sd.mesh_params.mesh_gate_announcements` value.
func ProtoToMeshRole(r setupv1.MeshRole) string {
	switch r {
	case setupv1.MeshRole_MESH_ROLE_MESH_POINT:
		return "0"
	case setupv1.MeshRole_MESH_ROLE_MESH_GATE:
		return "1"
	default:
		return ""
	}
}

// MeshRoleToProto maps a UCI mesh_gate_announcements value back to
// the MeshRole enum. Unknown inputs return UNSPECIFIED.
func MeshRoleToProto(s string) setupv1.MeshRole {
	switch s {
	case "0":
		return setupv1.MeshRole_MESH_ROLE_MESH_POINT
	case "1":
		return setupv1.MeshRole_MESH_ROLE_MESH_GATE
	default:
		return setupv1.MeshRole_MESH_ROLE_UNSPECIFIED
	}
}

// ProtoToMeshPointMode maps the MeshPointMode enum onto the UCI
// `network.wizard.device_mode_meshpoint` value used by the
// settings UI.
func ProtoToMeshPointMode(m setupv1.MeshPointMode) string {
	switch m {
	case setupv1.MeshPointMode_MESH_POINT_MODE_NONE:
		return "none"
	case setupv1.MeshPointMode_MESH_POINT_MODE_EXTENDER:
		return "extender"
	default:
		return ""
	}
}

// MeshPointModeToProto maps a UCI device_mode_meshpoint value back
// to the MeshPointMode enum. Unknown inputs return UNSPECIFIED.
func MeshPointModeToProto(s string) setupv1.MeshPointMode {
	switch strings.ToLower(s) {
	case "none":
		return setupv1.MeshPointMode_MESH_POINT_MODE_NONE
	case "extender":
		return setupv1.MeshPointMode_MESH_POINT_MODE_EXTENDER
	default:
		return setupv1.MeshPointMode_MESH_POINT_MODE_UNSPECIFIED
	}
}

// ProtoToMeshGateMode maps the MeshGateMode enum onto the UCI
// `network.wizard.device_mode_meshgate` value.
func ProtoToMeshGateMode(m setupv1.MeshGateMode) string {
	switch m {
	case setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER:
		return "router"
	case setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER_FIREWALL:
		return "router_firewall"
	default:
		return ""
	}
}

// MeshGateModeToProto maps a UCI device_mode_meshgate value back to
// the MeshGateMode enum. Unknown inputs return UNSPECIFIED.
func MeshGateModeToProto(s string) setupv1.MeshGateMode {
	switch strings.ToLower(s) {
	case "router":
		return setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER
	case "router_firewall":
		return setupv1.MeshGateMode_MESH_GATE_MODE_ROUTER_FIREWALL
	default:
		return setupv1.MeshGateMode_MESH_GATE_MODE_UNSPECIFIED
	}
}

// ProtoToUplinkType maps the UplinkType enum onto the UCI
// `network.wizard.uplink` prefix used by the LuCI Morse wizard.
// Callers append a port/radio suffix (e.g. "-eth0", "-radio2-sta")
// when one is supplied via Uplink.ethernet_port or
// Uplink.wireless.radio_name.
func ProtoToUplinkType(u setupv1.UplinkType) string {
	switch u {
	case setupv1.UplinkType_UPLINK_TYPE_ETHERNET:
		return "ethernet"
	case setupv1.UplinkType_UPLINK_TYPE_WIRELESS_STA:
		return "wifi-sta"
	default:
		return ""
	}
}

// UplinkTypeToProto maps a UCI uplink prefix back to the UplinkType
// enum. Unknown inputs return UNSPECIFIED.
func UplinkTypeToProto(s string) setupv1.UplinkType {
	switch strings.ToLower(s) {
	case "ethernet":
		return setupv1.UplinkType_UPLINK_TYPE_ETHERNET
	case "wifi-sta", "wireless-sta":
		return setupv1.UplinkType_UPLINK_TYPE_WIRELESS_STA
	default:
		return setupv1.UplinkType_UPLINK_TYPE_UNSPECIFIED
	}
}

// ─── GetSetupStatus ───────────────────────────────────────────────────────────

// GetSetupStatus reports whether the wizard is reachable on this
// device and provides the hardware context (radios, ethernet ports,
// hostname) the wizard needs to render its first screen.
func (s *SetupService) GetSetupStatus(ctx context.Context, _ *emptypb.Empty) (*setupv1.GetSetupStatusResponse, error) {
	s.Log.Debug().Msg("GetSetupStatus request received")

	resp := &setupv1.GetSetupStatusResponse{
		IsEnabled:        s.Cfg.GetSetupEnabled(),
		IsSetupComplete:  s.Cfg.GetSetupComplete(),
		CurrentHostname:  s.currentHostname(),
		Radios:           s.collectSetupRadios(ctx),
		EthernetPorts:    s.collectEthernetPorts(),
	}

	resp.HasHalowRadio = anyHalow(resp.Radios)
	resp.AlreadyConfigured = s.detectAlreadyConfigured(resp.CurrentHostname)

	return resp, nil
}

// currentHostname reads `system.@system[0].hostname` via the UCI
// reader, returning empty if the section cannot be read.
func (s *SetupService) currentHostname() string {
	cfg, err := network.GetSystemConfigWithReader(s.UCI)
	if err != nil {
		s.Log.Warn().Err(err).Msg("reading system hostname")

		return ""
	}

	return cfg.Hostname
}

// collectSetupRadios returns one SetupRadio entry per `wifi-device`
// in the wireless UCI config, marking radios with `option type 'morse'`
// as HaLow. Bandwidths/channels per radio are not yet populated; the
// wizard handler defers to WifiConfigService.GetRadioSettings for the
// channel list (frontend calls both RPCs in parallel).
func (s *SetupService) collectSetupRadios(_ context.Context) []*setupv1.SetupRadio {
	deviceSections, err := s.UCI.GetSections("wireless", "wifi-device")
	if err != nil {
		s.Log.Warn().Err(err).Msg("listing wifi-device sections")

		return nil
	}

	radios := make([]*setupv1.SetupRadio, 0, len(deviceSections))

	for _, name := range deviceSections {
		dev, err := network.GetWirelessDeviceByNameWithReader(name, s.UCI)
		if err != nil {
			continue
		}

		radios = append(radios, &setupv1.SetupRadio{
			Name:         name,
			Band:         dev.Band,
			HardwareName: dev.Path,
			IsHalow:      strings.EqualFold(dev.Type, "morse"),
		})
	}

	return radios
}

// collectEthernetPorts enumerates the ethernet interfaces that the
// wizard's uplink step lets the operator choose from. Pulled from
// the existing InterfaceProvider so the wizard sees the same view
// the dashboard does.
func (s *SetupService) collectEthernetPorts() []string {
	if s.Interfaces == nil {
		return nil
	}

	all, err := s.Interfaces.ListAll()
	if err != nil {
		s.Log.Warn().Err(err).Msg("listing network interfaces")

		return nil
	}

	out := make([]string, 0, len(all))

	for _, info := range all {
		if isLikelyEthernetPort(info.Name) {
			out = append(out, info.Name)
		}
	}

	return out
}

// isLikelyEthernetPort returns true for interface names that look
// like physical ethernet ports. Conservative heuristic: starts with
// "eth", "lan", "en", or matches "wan*". Excludes wireless (wlan*)
// and bridges (br*) and batman (bat*, batmesh*).
func isLikelyEthernetPort(name string) bool {
	if name == "" {
		return false
	}

	switch {
	case strings.HasPrefix(name, "wlan"),
		strings.HasPrefix(name, "br"),
		strings.HasPrefix(name, "bat"),
		strings.HasPrefix(name, "lo"),
		strings.HasPrefix(name, "tun"),
		strings.HasPrefix(name, "tap"),
		strings.HasPrefix(name, "vxlan"),
		strings.HasPrefix(name, "tailscale"):
		return false
	}

	return strings.HasPrefix(name, "eth") ||
		strings.HasPrefix(name, "lan") ||
		strings.HasPrefix(name, "en") ||
		strings.HasPrefix(name, "wan")
}

// anyHalow reports whether the supplied radio list contains at least
// one HaLow (802.11ah / S1G) radio.
func anyHalow(radios []*setupv1.SetupRadio) bool {
	for _, r := range radios {
		if r.IsHalow {
			return true
		}
	}

	return false
}

// detectAlreadyConfigured implements the heuristic described in the
// plan: a device looks already-configured if any of the following are
// true:
//   - mesh11sd.mesh_params.mesh_gate_announcements is set
//   - network.ahwlan.proto is set
//   - auth.enable is true
//   - the system hostname does NOT match the factory pattern
func (s *SetupService) detectAlreadyConfigured(currentHostname string) bool {
	if s.Cfg.GetAuthEnable() {
		return true
	}

	if v, ok := s.UCI.Get("mesh11sd", "mesh_params", "mesh_gate_announcements"); ok && len(v) > 0 && v[0] != "" {
		return true
	}

	if v, ok := s.UCI.Get("network", "ahwlan", "proto"); ok && len(v) > 0 && v[0] != "" {
		return true
	}

	if currentHostname != "" && !factoryHostnamePattern.MatchString(currentHostname) {
		return true
	}

	return false
}

// ─── ApplySetup (placeholder; phases land in subsequent rounds) ──────────────

// applySetupStream is the narrow Send-only interface the phase
// helpers use. The connect.ServerStream returned by the wire layer
// satisfies it. Tests substitute a fake collector to inspect the
// event sequence without standing up an httptest server.
type applySetupStream interface {
	Send(*setupv1.ApplySetupResponse) error
}

// ApplySetup atomically applies a complete MeshNodeProfile. Each
// phase emits a STATUS_STARTED event on entry and STATUS_DONE or
// STATUS_FAILED on exit. The terminal event (PHASE_TERMINAL) is
// emitted before service reloads so the frontend has a clean signal
// even if reloads sever the streaming connection.
//
// Phase implementations land in setup_phases.go in subsequent rounds.
func (s *SetupService) ApplySetup(
	ctx context.Context,
	req *connect.Request[setupv1.ApplySetupRequest],
	stream *connect.ServerStream[setupv1.ApplySetupResponse],
) error {
	return s.applySetup(ctx, req.Msg.GetProfile(), stream)
}

// applySetup is the testable core of ApplySetup. It accepts the
// narrow applySetupStream interface so unit tests can drive it
// without a real connect ServerStream (which has no public
// constructor).
func (s *SetupService) applySetup(
	ctx context.Context,
	profile *setupv1.MeshNodeProfile,
	stream applySetupStream,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if profile == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("profile is required"))
	}

	// Phase 0: re-apply guard. Refuse if disabled or already complete.
	if err := s.checkReapplyGuard(ctx); err != nil {
		return err
	}

	// Phase 1: validation (cross-field invariants beyond what
	// buf.validate enforces). Emits STARTED/DONE events around the
	// validation block.
	if err := s.runValidate(ctx, stream, profile); err != nil {
		return s.emitTerminal(stream, setupv1.ApplySetupResponse_PHASE_VALIDATE, err, profile)
	}

	// TODO(phase-2-onwards): snapshot UCI, run reset+mutation phases,
	// commit, set password, persist flags, emit terminal, kick off
	// reload goroutine. Implemented in subsequent rounds.
	_ = ctx

	// Until the rest of the phases land, return Unimplemented so
	// callers see a clear signal rather than an empty success.
	return connect.NewError(connect.CodeUnimplemented,
		fmt.Errorf("ApplySetup phases beyond validation are not yet implemented"))
}

// checkReapplyGuard rejects ApplySetup when the wizard is disabled
// (CodeUnavailable) or already complete (CodeFailedPrecondition).
// The middleware applies the same gate; this is defense-in-depth.
func (s *SetupService) checkReapplyGuard(_ context.Context) error {
	if !s.Cfg.GetSetupEnabled() {
		return connect.NewError(connect.CodeUnavailable,
			errors.New("setup wizard is disabled on this device"))
	}

	if s.Cfg.GetSetupComplete() {
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("setup wizard already completed on this device"))
	}

	if !s.hasUsableHalowRadio() {
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no usable HaLow radio detected on this device"))
	}

	return nil
}

// hasUsableHalowRadio returns true iff at least one wifi-device has
// `option type 'morse'`. The plan additionally calls for an iwinfo
// liveness check; that lands when the iwinfo wiring is verified end
// to end against a real radio.
func (s *SetupService) hasUsableHalowRadio() bool {
	deviceSections, err := s.UCI.GetSections("wireless", "wifi-device")
	if err != nil {
		return false
	}

	for _, name := range deviceSections {
		typ, ok := s.UCI.Get("wireless", name, "type")
		if !ok || len(typ) == 0 {
			continue
		}

		if strings.EqualFold(typ[0], "morse") {
			return true
		}
	}

	return false
}

// runValidate emits STATUS_STARTED, performs cross-field validation,
// and emits STATUS_DONE on success or STATUS_FAILED on validation
// failure. Returns the validation error so the caller can decide
// whether to roll back.
func (s *SetupService) runValidate(
	_ context.Context,
	stream applySetupStream,
	profile *setupv1.MeshNodeProfile,
) error {
	if err := emitPhaseStarted(stream, setupv1.ApplySetupResponse_PHASE_VALIDATE,
		"validating profile"); err != nil {
		return err
	}

	if err := s.validateProfile(profile); err != nil {
		_ = emitPhaseFailed(stream, setupv1.ApplySetupResponse_PHASE_VALIDATE, err.Error())

		return err
	}

	return emitPhaseDone(stream, setupv1.ApplySetupResponse_PHASE_VALIDATE, "profile valid")
}

// validateProfile checks the cross-field invariants that buf.validate
// can't express on its own.
//
//nolint:gocyclo // a flat list of independent checks is more readable than a function per check
func (s *SetupService) validateProfile(profile *setupv1.MeshNodeProfile) error {
	if !hostnameRFC1123.MatchString(profile.GetHostname()) {
		return fmt.Errorf("hostname %q is not a valid RFC 1123 label",
			profile.GetHostname())
	}

	role := profile.GetRole()
	if role == setupv1.MeshRole_MESH_ROLE_UNSPECIFIED {
		return errors.New("role is required")
	}

	switch role {
	case setupv1.MeshRole_MESH_ROLE_MESH_GATE:
		if profile.GetMeshgateMode() == setupv1.MeshGateMode_MESH_GATE_MODE_UNSPECIFIED {
			return errors.New("meshgate_mode is required when role is MESH_GATE")
		}

		if profile.GetMeshpointMode() != setupv1.MeshPointMode_MESH_POINT_MODE_UNSPECIFIED {
			return errors.New("meshpoint_mode must not be set when role is MESH_GATE")
		}

		if profile.GetUplink() == nil ||
			profile.GetUplink().GetType() == setupv1.UplinkType_UPLINK_TYPE_UNSPECIFIED {
			return errors.New("uplink is required when role is MESH_GATE")
		}
	case setupv1.MeshRole_MESH_ROLE_MESH_POINT:
		if profile.GetMeshpointMode() == setupv1.MeshPointMode_MESH_POINT_MODE_UNSPECIFIED {
			return errors.New("meshpoint_mode is required when role is MESH_POINT")
		}

		if profile.GetMeshgateMode() != setupv1.MeshGateMode_MESH_GATE_MODE_UNSPECIFIED {
			return errors.New("meshgate_mode must not be set when role is MESH_POINT")
		}
	}

	mesh := profile.GetMesh()
	if mesh == nil || mesh.GetRadioName() == "" {
		return errors.New("mesh radio configuration is required")
	}

	if !s.radioExistsAsMorse(mesh.GetRadioName()) {
		return fmt.Errorf("mesh radio %q is not a HaLow (type=morse) wifi-device", mesh.GetRadioName())
	}

	if err := validateMeshChannelBandwidth(mesh.GetBandwidthMhz(), mesh.GetChannel()); err != nil {
		return err
	}

	if err := validateAPs(profile.GetAps(), mesh.GetRadioName()); err != nil {
		return err
	}

	if err := validateUplink(profile.GetUplink(), mesh.GetRadioName(), profile.GetAps()); err != nil {
		return err
	}

	if err := validatePassphrase(mesh.GetEncryption(), mesh.GetPassphrase()); err != nil {
		return fmt.Errorf("mesh: %w", err)
	}

	if len(profile.GetAdminPassword()) < 8 {
		return errors.New("admin_password must be at least 8 characters")
	}

	return nil
}

// radioExistsAsMorse confirms the named wifi-device exists and has
// `option type 'morse'`. Phase 1 cross-field guard.
func (s *SetupService) radioExistsAsMorse(radioName string) bool {
	if radioName == "" {
		return false
	}

	typ, ok := s.UCI.Get("wireless", radioName, "type")
	if !ok || len(typ) == 0 {
		return false
	}

	return strings.EqualFold(typ[0], "morse")
}

// validateMeshChannelBandwidth applies a minimal sanity check on
// the (channel, bandwidth) pair. Full regulatory validation against
// `WifiConfigService.GetRadioSettings` is deferred to the handler's
// later phase when the radio settings are loaded; the bandwidth must
// at least be one of the legal S1G / VHT values.
func validateMeshChannelBandwidth(bandwidthMHz, channel uint32) error {
	if channel == 0 {
		return errors.New("mesh channel is required")
	}

	switch bandwidthMHz {
	case 1, 2, 4, 8, 20, 40, 80, 160:
		return nil
	default:
		return fmt.Errorf("mesh bandwidth %d MHz is not a supported value", bandwidthMHz)
	}
}

// validateAPs enforces uniqueness of AP radio names and that none
// equals the mesh radio.
func validateAPs(aps []*setupv1.RadioApProfile, meshRadio string) error {
	seen := make(map[string]struct{}, len(aps))

	for _, ap := range aps {
		name := ap.GetRadioName()
		if name == "" {
			return errors.New("AP radio_name is required")
		}

		if name == meshRadio {
			return fmt.Errorf("AP radio %q must differ from the mesh radio", name)
		}

		if _, dup := seen[name]; dup {
			return fmt.Errorf("AP radio %q listed more than once", name)
		}

		seen[name] = struct{}{}

		if !ap.GetEnabled() {
			continue
		}

		if err := validatePassphrase(ap.GetEncryption(), ap.GetPassphrase()); err != nil {
			return fmt.Errorf("AP %q: %w", name, err)
		}
	}

	return nil
}

// validateUplink confirms the wireless STA radio (when uplink type
// is WIRELESS_STA) is not the mesh radio and not also configured as
// an enabled AP — concurrent AP+STA on a single radio is not
// supported by the wizard.
func validateUplink(uplink *setupv1.Uplink, meshRadio string, aps []*setupv1.RadioApProfile) error {
	if uplink == nil ||
		uplink.GetType() != setupv1.UplinkType_UPLINK_TYPE_WIRELESS_STA {
		return nil
	}

	wireless := uplink.GetWireless()
	if wireless == nil || wireless.GetRadioName() == "" {
		return errors.New("uplink.wireless.radio_name is required for wireless STA uplink")
	}

	if wireless.GetRadioName() == meshRadio {
		return fmt.Errorf("uplink STA radio %q must differ from the mesh radio", wireless.GetRadioName())
	}

	for _, ap := range aps {
		if ap.GetEnabled() && ap.GetRadioName() == wireless.GetRadioName() {
			return fmt.Errorf("uplink STA radio %q is also configured as an enabled AP",
				wireless.GetRadioName())
		}
	}

	if err := validatePassphrase(wireless.GetEncryption(), wireless.GetPassphrase()); err != nil {
		return fmt.Errorf("uplink: %w", err)
	}

	return nil
}

// validatePassphrase enforces WPA's 8..63 char rule unless the
// chosen encryption is open (NONE or OWE).
func validatePassphrase(enc wificonfigv1.WifiEncryption, pass string) error {
	switch enc {
	case wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_NONE,
		wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_OWE:
		return nil
	}

	if l := len(pass); l < 8 || l > 63 {
		return fmt.Errorf("passphrase length %d not in [8,63]", l)
	}

	return nil
}

// emitPhaseStarted/Done/Failed encapsulate the boilerplate for
// emitting per-phase events on the streaming response.
func emitPhaseStarted(stream applySetupStream,
	phase setupv1.ApplySetupResponse_Phase, message string) error {
	return stream.Send(&setupv1.ApplySetupResponse{
		Phase:   phase,
		Status:  setupv1.ApplySetupResponse_STATUS_STARTED,
		Message: message,
	})
}

func emitPhaseDone(stream applySetupStream,
	phase setupv1.ApplySetupResponse_Phase, message string) error {
	return stream.Send(&setupv1.ApplySetupResponse{
		Phase:   phase,
		Status:  setupv1.ApplySetupResponse_STATUS_DONE,
		Message: message,
	})
}

func emitPhaseFailed(stream applySetupStream,
	phase setupv1.ApplySetupResponse_Phase, message string) error {
	return stream.Send(&setupv1.ApplySetupResponse{
		Phase:   phase,
		Status:  setupv1.ApplySetupResponse_STATUS_FAILED,
		Message: message,
	})
}

// emitTerminal sends the final PHASE_TERMINAL event reporting the
// failed phase (when called from a failure path) and returns a
// CodeInternal error wrapping the underlying cause.
func (s *SetupService) emitTerminal(
	stream applySetupStream,
	failedPhase setupv1.ApplySetupResponse_Phase,
	cause error,
	profile *setupv1.MeshNodeProfile,
) error {
	_ = stream.Send(&setupv1.ApplySetupResponse{
		Phase:  setupv1.ApplySetupResponse_PHASE_TERMINAL,
		Status: setupv1.ApplySetupResponse_STATUS_FAILED,
		Result: &setupv1.ApplySetupResult{
			Success:      false,
			FailedPhase:  failedPhase,
			ExpectedSsid: expectedSSID(profile),
			ExpectedUrl:  expectedURL(profile),
		},
	})

	// Surface validation failures as InvalidArgument; everything else
	// becomes Internal.
	if failedPhase == setupv1.ApplySetupResponse_PHASE_VALIDATE {
		return connect.NewError(connect.CodeInvalidArgument, cause)
	}

	return connect.NewError(connect.CodeInternal, cause)
}

// expectedSSID returns the SSID of the highest-priority enabled AP
// in the profile, or "" if no enabled AP is configured.
func expectedSSID(profile *setupv1.MeshNodeProfile) string {
	for _, ap := range profile.GetAps() {
		if ap.GetEnabled() && ap.GetSsid() != "" {
			return ap.GetSsid()
		}
	}

	return ""
}

// expectedURL returns the mDNS URL the device will be reachable at
// after the wizard's hostname write.
func expectedURL(profile *setupv1.MeshNodeProfile) string {
	if profile.GetHostname() == "" {
		return ""
	}

	return "https://" + profile.GetHostname() + ".local:8081/login"
}
