package comms

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/webaudio"
)

// Service is the live comms subsystem instance returned (conceptually) by
// Start. It carries an immutable *CommsConfig snapshot and the live
// *CommsRuntime, and exposes the accessor methods that the HTTP handlers
// need to read or mutate runtime state.
//
// Cfg is set once at construction and never replaced; Rt is set once after
// Start finishes building the runtime and is cleared on shutdown via
// SetDefault(nil). Methods on *Service tolerate nil receivers and a nil
// Service.Rt so handlers can defensively call them before comms is enabled.
type Service struct {
	Cfg *CommsConfig
	Rt  *CommsRuntime
}

// defaultService is the process-wide singleton most recently published by
// Start. It exists so external call sites that do not yet receive a
// *Service by injection (handlers, tests) can resolve the live instance
// lazily via Default(). Phase D2 of the comms refactor adds a typed
// `Service func() *comms.Service` field to the HTTP handler so handlers
// no longer need to reach in here directly.
//
// Access is lock-free via atomic.Pointer: Start publishes with Store, every
// accessor reads with Load. The pointer itself is the only mutable state;
// the referent *Service is fully built before publication and its own
// fields carry their own synchronization.
//
//nolint:gochecknoglobals // injection-point shim — see Default / SetDefault.
var defaultService atomic.Pointer[Service]

// ErrNotRunning is returned by Service methods when the comms subsystem has
// not been started (or has been stopped). It is exported so callers can use
// errors.Is to distinguish it from other failure modes.
var ErrNotRunning = errors.New("comms: subsystem is not running")

// Default returns the Service most recently published by Start, or nil when
// comms has not been started (or has stopped).
func Default() *Service {
	return defaultService.Load()
}

// SetDefault publishes svc as the process-wide default Service. Start calls
// this after constructing the runtime; shutdown passes nil. Tests that
// build a Service directly use it the same way to wire up the lazy lookup.
func SetDefault(svc *Service) {
	defaultService.Store(svc)
}

// ─── Service methods ─────────────────────────────────────────────────────────

// ActiveMulticastAddr returns the multicast group address of the first
// configured port. Returns "" when no ports are configured.
func (s *Service) ActiveMulticastAddr() string {
	if s == nil || s.Cfg == nil || len(s.Cfg.McastPorts) == 0 {
		return ""
	}

	return s.Cfg.McastPorts[0].Address
}

// ActiveMulticastPort returns the UDP port of the first configured port.
// Returns 0 when no ports are configured.
func (s *Service) ActiveMulticastPort() int {
	if s == nil || s.Cfg == nil || len(s.Cfg.McastPorts) == 0 {
		return 0
	}

	return s.Cfg.McastPorts[0].Port
}

// EnableTalkGroupSend toggles RTP transmission on the port at portIdx.
func (s *Service) EnableTalkGroupSend(portIdx int, enabled bool) error {
	if s == nil || s.Rt == nil {
		return ErrNotRunning
	}

	if portIdx < 0 || portIdx >= len(s.Rt.Ports) {
		return fmt.Errorf("comms: port index %d out of range [0, %d)", portIdx, len(s.Rt.Ports))
	}

	s.Rt.Ports[portIdx].SendEnabled.Store(enabled)

	return nil
}

// EnableTalkGroupReceive toggles RTP reception on the port at portIdx.
func (s *Service) EnableTalkGroupReceive(portIdx int, enabled bool) error {
	if s == nil || s.Rt == nil {
		return ErrNotRunning
	}

	if portIdx < 0 || portIdx >= len(s.Rt.Ports) {
		return fmt.Errorf("comms: port index %d out of range [0, %d)", portIdx, len(s.Rt.Ports))
	}

	s.Rt.Ports[portIdx].ReceiveEnabled.Store(enabled)

	return nil
}

// TalkGroupStates returns a snapshot of per-port direction-toggle state.
func (s *Service) TalkGroupStates() ([]McastPortState, error) {
	if s == nil || s.Rt == nil {
		return nil, ErrNotRunning
	}

	states := make([]McastPortState, len(s.Rt.Ports))

	for i, pc := range s.Rt.Ports {
		states[i] = McastPortState{
			Address:        pc.cfg.Address,
			Port:           pc.cfg.Port,
			SendEnabled:    pc.SendEnabled.Load(),
			ReceiveEnabled: pc.ReceiveEnabled.Load(),
		}
	}

	return states, nil
}

// WebEventSource returns the web control source if one was constructed,
// otherwise nil.
func (s *Service) WebEventSource() *control.WebEventSource {
	if s == nil || s.Rt == nil {
		return nil
	}

	return s.Rt.WebEvtSrc
}

// WebAudioBridge returns the web audio bridge if one was constructed,
// otherwise nil.
func (s *Service) WebAudioBridge() *webaudio.Bridge {
	if s == nil || s.Rt == nil {
		return nil
	}

	return s.Rt.WebBridge
}
