package comms

import (
	"errors"

	"github.com/openmanet/openmanetd/internal/comms/control"
)

// This file wires the in-package control source constructors into the
// control.Registry. It lives in the comms package (not internal/comms/control)
// because the constructors themselves still live here in Phase 2 of the comms
// refactor — moving them across would create an import cycle since control is
// already imported by comms via event_alias.go. A future phase will relocate
// the backends into internal/comms/control and these inits will move with
// them.

// openvlmBackend is the ControlDeps.Backend payload for the OpenVLM HID source.
type openvlmBackend struct{}

// roipBackend is the ControlDeps.Backend payload for the ROIP bridge source.
// All fields are required.
type roipBackend struct {
	Cfg            *CommsConfig
	IsReceiving    func() bool
	IsBroadcasting func() bool
	SetTap         func(chan []float32)
	ClearTap       func()
}

// webBackend is the ControlDeps.Backend payload for the web RPC source. The
// factory writes the constructed *webEventSource back into Sink so the rest
// of the comms runtime can find it.
type webBackend struct {
	Sink func(*webEventSource)
}

// nanopttBackend is the ControlDeps.Backend payload for the evdev NanoPTT
// source.
type nanopttBackend struct {
	Cfg *CommsConfig
}

func init() {
	control.Register("openvlm", func(deps control.ControlDeps) (control.EventSource, error) {
		return NewOpenVLMSource(deps.Log), nil
	})

	control.Register(controlSourceROIP, func(deps control.ControlDeps) (control.EventSource, error) {
		b, ok := deps.Backend.(*roipBackend)
		if !ok || b == nil {
			return nil, errors.New("comms: roip control source missing backend deps")
		}

		return NewROIPSource(b.Cfg, b.IsReceiving, b.IsBroadcasting, b.SetTap, b.ClearTap, deps.Log), nil
	})

	control.Register(controlSourceWeb, func(deps control.ControlDeps) (control.EventSource, error) {
		b, ok := deps.Backend.(*webBackend)
		if !ok || b == nil {
			return nil, errors.New("comms: web control source missing backend deps")
		}

		ws := NewWebEventSource(deps.Log)
		if b.Sink != nil {
			b.Sink(ws)
		}

		return ws, nil
	})

	control.Register(defaultControlSourceNanoPTT, func(deps control.ControlDeps) (control.EventSource, error) {
		b, ok := deps.Backend.(*nanopttBackend)
		if !ok || b == nil {
			return nil, errors.New("comms: nanoptt control source missing backend deps")
		}

		dev := b.Cfg.findCommDevice()
		if dev == nil {
			return nil, errors.New("comms: PTT device not found")
		}

		deps.Log.Info().Msgf("comms: PTT on evdev device: %s", dev.Name)

		return control.NewNanoPTTSource(dev, b.Cfg.CommKey, deps.Log), nil
	})
}

// controlLookup is a thin wrapper around control.Lookup so the comms package
// does not have to import the registry name in multiple files.
func controlLookup(name string) (control.Factory, bool) {
	return control.Lookup(name)
}

// buildControlDeps assembles the ControlDeps payload for the configured
// ControlSource. The handled return value is false when the source name is
// unknown to this helper, in which case the caller falls back to the legacy
// switch in buildEventSource.
func (cfg *CommsConfig) buildControlDeps(rt *CommsRuntime) (control.ControlDeps, string, bool) {
	deps := control.ControlDeps{Log: cfg.Log}

	switch cfg.ControlSource {
	case defaultCtrlSrc:
		deps.Backend = &openvlmBackend{}

		return deps, defaultCtrlSrc, true

	case controlSourceROIP:
		deps.Backend = &roipBackend{
			Cfg:            cfg,
			IsReceiving:    func() bool { return cfg.isReceivingRemote(rt) },
			IsBroadcasting: func() bool { return rt.broadcasting.Load() },
			SetTap:         func(ch chan []float32) { rt.broadcastTap.Store(&ch) },
			ClearTap:       func() { rt.broadcastTap.Store(nil) },
		}

		return deps, controlSourceROIP, true

	case controlSourceWeb:
		deps.Backend = &webBackend{
			Sink: func(ws *webEventSource) { rt.webEvtSrc = ws },
		}

		return deps, controlSourceWeb, true

	case defaultControlSourceNanoPTT:
		deps.Backend = &nanopttBackend{Cfg: cfg}

		return deps, defaultControlSourceNanoPTT, true
	}

	return deps, "", false
}

// logControlSource emits the same informational log line that the legacy
// switch in buildEventSource produced for each backend, preserving operator
// visibility during the registry rollout.
func (cfg *CommsConfig) logControlSource(name string) {
	switch name {
	case defaultCtrlSrc:
		cfg.Log.Info().Msgf("comms: PTT on OpenVLM HID dongle (VID=0x%04X PID=0x%04X)",
			openvlmVendorID, openvlmProductID)
	case controlSourceROIP:
		cfg.Log.Info().Msgf(
			"comms: ROIP bridge on OpenVLM (VID=0x%04X PID=0x%04X) COSmask=0x%02X VOX=%.3f hold=%s",
			openvlmVendorID, openvlmProductID, cfg.ROIPCOSGPIOMask, cfg.ROIPVOXThreshold, cfg.ROIPVOXHoldTime,
		)
	case controlSourceWeb:
		cfg.Log.Info().Msg("comms: PTT via web RPC")
	}
}

// Validate checks the comms configuration for self-consistency. Phase 2 of
// the comms refactor introduced this method specifically to verify that the
// configured ControlSource maps to a registered backend. Additional checks
// will be added by later phases. The method intentionally tolerates the
// empty string (treated as the openvlm default) so callers may invoke it
// either before or after normalizeControlSource has run.
func (cfg *CommsConfig) Validate() error {
	name := normalizeControlSource(cfg.ControlSource)
	if _, ok := control.Lookup(name); !ok {
		return errors.New("comms: unknown ControlSource " + name)
	}

	return nil
}

