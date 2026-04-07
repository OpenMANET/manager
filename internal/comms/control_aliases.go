package comms

import (
	"github.com/openmanet/openmanetd/internal/comms/control"
)

// This file provides parent-package aliases for symbols that moved into
// internal/comms/control so that files remaining in the parent (roip.go,
// comms.go, service.go, tests) keep compiling without churn.

// ─── OpenVLM constants (mirrored from control package) ──────────────────────

const (
	openvlmVendorID      = control.OpenVLMVendorID
	openvlmProductID     = control.OpenVLMProductID
	openvlmReportSize    = control.OpenVLMReportSize
	openvlmPayloadOffset = control.OpenVLMPayloadOffset
	openvlmGPIO3Mask     = control.OpenVLMGPIO3Mask
)

// ─── HID abstractions ───────────────────────────────────────────────────────

// HIDDevice is a parent-package alias for control.HIDDevice.
type HIDDevice = control.HIDDevice

// HIDOpener is a parent-package alias for control.HIDOpener.
type HIDOpener = control.HIDOpener

// defaultHIDOpener is the production HIDOpener (delegates to the control package).
var defaultHIDOpener HIDOpener = control.DefaultHIDOpener

// ─── Web event source alias ─────────────────────────────────────────────────

// webEventSource is a parent-package alias for control.WebEventSource.
type webEventSource = control.WebEventSource
