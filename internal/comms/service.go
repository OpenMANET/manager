package comms

import (
	"errors"
	"fmt"
	"sync/atomic"
)

// Service is the live comms subsystem instance returned (conceptually) by
// Start. It carries configuration, runtime state, and accessor methods for
// all control-plane operations the HTTP handlers need.
//
// During Phase 4 of the comms refactor Service is a type alias for
// CommsConfig: the existing CommsConfig already owns the runtime pointer, so
// aliasing lets us grow method surface on *Service without renaming the many
// existing receivers on *CommsConfig or re-plumbing every call site in one
// change. Subsequent phases will separate static config from live service
// state into distinct types.
type Service = CommsConfig

// defaultService is the process-wide singleton most recently published by
// Start. It exists only as the Phase 4 shim for external call sites (notably
// internal/openmanet/server/handlers/comms.go) that still reach into the
// comms package via free functions. Phase 5+ will thread a *Service through
// the handler struct and delete this global.
//
// Access is lock-free via atomic.Pointer: Start publishes with Store, every
// accessor reads with Load. The pointer itself is the only mutable state;
// the referent *Service is constructed before publication and its own
// fields carry their own synchronization.
//
//nolint:gochecknoglobals // shim — Phase 4 of comms refactor; remove in Phase 5+.
var defaultService atomic.Pointer[Service]

// Default returns the Service most recently published by Start, or nil when
// comms has not been started (or has stopped). Shim for Phase 4; callers in
// subsequent phases should receive a *Service directly.
func Default() *Service {
	return defaultService.Load()
}

// SetDefault publishes svc as the process-wide default Service. Start calls
// this after constructing the runtime; Stop-style cleanup passes nil. It is
// also used by tests that build a Service directly with newTestService.
func SetDefault(svc *Service) {
	defaultService.Store(svc)
}

// ─── Service methods ─────────────────────────────────────────────────────────
//
// Each method below has a matching package-level free function that
// delegates to Default(). Handlers will migrate to the methods in a later
// phase; the free functions remain as the shim surface.

// ActiveMulticastAddr returns the multicast group address of the first
// configured port. Returns "" when no ports are configured.
func (s *Service) ActiveMulticastAddr() string {
	if s == nil || len(s.McastPorts) == 0 {
		return ""
	}

	return s.McastPorts[0].Address
}

// ActiveMulticastPort returns the UDP port of the first configured port.
// Returns 0 when no ports are configured.
func (s *Service) ActiveMulticastPort() int {
	if s == nil || len(s.McastPorts) == 0 {
		return 0
	}

	return s.McastPorts[0].Port
}

// EnableTalkGroupSend toggles RTP transmission on the port at portIdx.
func (s *Service) EnableTalkGroupSend(portIdx int, enabled bool) error {
	if s == nil || s.runtime == nil {
		return errors.New("comms: subsystem is not running")
	}

	rt := s.runtime
	if portIdx < 0 || portIdx >= len(rt.ports) {
		return fmt.Errorf("comms: port index %d out of range [0, %d)", portIdx, len(rt.ports))
	}

	rt.ports[portIdx].sendEnabled.Store(enabled)

	return nil
}

// EnableTalkGroupReceive toggles RTP reception on the port at portIdx.
func (s *Service) EnableTalkGroupReceive(portIdx int, enabled bool) error {
	if s == nil || s.runtime == nil {
		return errors.New("comms: subsystem is not running")
	}

	rt := s.runtime
	if portIdx < 0 || portIdx >= len(rt.ports) {
		return fmt.Errorf("comms: port index %d out of range [0, %d)", portIdx, len(rt.ports))
	}

	rt.ports[portIdx].receiveEnabled.Store(enabled)

	return nil
}

// TalkGroupStates returns a snapshot of per-port direction-toggle state.
func (s *Service) TalkGroupStates() ([]McastPortState, error) {
	if s == nil || s.runtime == nil {
		return nil, errors.New("comms: subsystem is not running")
	}

	rt := s.runtime
	states := make([]McastPortState, len(rt.ports))

	for i, pc := range rt.ports {
		states[i] = McastPortState{
			Address:        pc.cfg.Address,
			Port:           pc.cfg.Port,
			SendEnabled:    pc.sendEnabled.Load(),
			ReceiveEnabled: pc.receiveEnabled.Load(),
		}
	}

	return states, nil
}

// WebEventSource returns the web control source if one was constructed,
// otherwise nil.
func (s *Service) WebEventSource() *webEventSource {
	if s == nil || s.runtime == nil {
		return nil
	}

	return s.runtime.webEvtSrc
}

// WebAudioBridge returns the web audio bridge if one was constructed,
// otherwise nil.
func (s *Service) WebAudioBridge() *WebAudioBridge {
	if s == nil || s.runtime == nil {
		return nil
	}

	return s.runtime.webBridge
}

// ─── Shim free functions ─────────────────────────────────────────────────────
//
// These preserve the pre-Phase-4 API that internal/openmanet/server/handlers
// depends on. They delegate to the current default Service. Remove in
// Phase 5+ once handlers receive a *Service by injection.

// GetActiveMulticastAddr is the shim for Service.ActiveMulticastAddr.
func GetActiveMulticastAddr() string { return Default().ActiveMulticastAddr() }

// GetActiveMulticastPort is the shim for Service.ActiveMulticastPort.
func GetActiveMulticastPort() int { return Default().ActiveMulticastPort() }

// EnableTalkGroupSend is the shim for Service.EnableTalkGroupSend.
func EnableTalkGroupSend(portIdx int, enabled bool) error {
	return Default().EnableTalkGroupSend(portIdx, enabled)
}

// EnableTalkGroupReceive is the shim for Service.EnableTalkGroupReceive.
func EnableTalkGroupReceive(portIdx int, enabled bool) error {
	return Default().EnableTalkGroupReceive(portIdx, enabled)
}

// GetTalkGroupStates is the shim for Service.TalkGroupStates.
func GetTalkGroupStates() ([]McastPortState, error) { return Default().TalkGroupStates() }

// GetWebEventSource is the shim for Service.WebEventSource.
func GetWebEventSource() *webEventSource { return Default().WebEventSource() }

// GetWebAudioBridge is the shim for Service.WebAudioBridge.
func GetWebAudioBridge() *WebAudioBridge { return Default().WebAudioBridge() }
