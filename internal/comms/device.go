package comms

import (
	"strings"

	evdev "github.com/gvalkov/golang-evdev"

	"github.com/openmanet/openmanetd/internal/comms/device"
)

const (
	defaultControlSourceOpenVLM = "openvlm"
	defaultControlSourceNanoPTT = "nanoptt"
	controlSourceROIP           = "roip"
	controlSourceWeb            = "web"
	controlSourceBluealsaXEvent = "bluealsa_xevent"
)

// normalizeControlSource maps raw config strings to canonical control source names.
// Unrecognized values (including empty string) default to "openvlm".
func normalizeControlSource(src string) string {
	switch strings.ToLower(strings.TrimSpace(src)) {
	case controlSourceBluealsaXEvent:
		return controlSourceBluealsaXEvent
	case defaultControlSourceNanoPTT:
		return defaultControlSourceNanoPTT
	case controlSourceROIP:
		return controlSourceROIP
	case controlSourceWeb:
		return controlSourceWeb
	default:
		return defaultControlSourceOpenVLM
	}
}

// findCommDevice searches for a Linux input device whose name matches
// cfg.NanoPTTDeviceName within the glob pattern cfg.NanoPTTDevicePath.
func (cfg *CommsConfig) findCommDevice() *evdev.InputDevice {
	return device.FindEvdev(cfg.NanoPTTDevicePath, cfg.NanoPTTDeviceName)
}
